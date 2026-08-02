package relay

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/fishaudio"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// fishVoiceTarget is the upstream account a voice management call must reach.
type fishVoiceTarget struct {
	baseURL   string
	apiKey    string
	channelId int
	proxy     string
}

// resolveTargetFromContext uses the channel the distributor selected. Only
// creation may pick a channel freely — every later call must return to the
// account that holds the voice.
//
// These handlers bypass the adaptor layer, which is what normally guarantees a
// channel's key only ever reaches its own provider. The channel type must
// therefore be checked here: channel selection matches on group and model name
// alone, so any channel listing the routing model — including one belonging to
// another provider entirely — can be picked, and its key would then be sent to
// api.fish.audio.
func resolveTargetFromContext(c *gin.Context) (fishVoiceTarget, error) {
	if channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType); channelType != constant.ChannelTypeFishAudio {
		return fishVoiceTarget{}, fmt.Errorf("selected channel is of type %d, not fish audio", channelType)
	}
	baseUrl := common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl)
	if baseUrl == "" {
		baseUrl = constant.ChannelBaseURLs[constant.ChannelTypeFishAudio]
	}
	target := fishVoiceTarget{
		baseURL:   baseUrl,
		apiKey:    common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		channelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
	}
	if channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
		target.proxy = channelSetting.Proxy
	}
	return target, nil
}

// resolveTargetFromChannel loads the channel a voice was created on, so reads,
// updates and deletes hit the same upstream account.
func resolveTargetFromChannel(channelId int) (fishVoiceTarget, error) {
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return fishVoiceTarget{}, fmt.Errorf("voice model channel #%d is unavailable: %w", channelId, err)
	}
	if channel.Type != constant.ChannelTypeFishAudio {
		return fishVoiceTarget{}, fmt.Errorf("channel #%d is no longer a fish audio channel", channelId)
	}
	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		return fishVoiceTarget{}, fmt.Errorf("channel #%d has no usable key", channelId)
	}
	baseUrl := channel.GetBaseURL()
	if baseUrl == "" {
		baseUrl = constant.ChannelBaseURLs[constant.ChannelTypeFishAudio]
	}
	return fishVoiceTarget{
		baseURL:   baseUrl,
		apiKey:    key,
		channelId: channel.Id,
		proxy:     channel.GetSetting().Proxy,
	}, nil
}

func doFishAudioRequest(c *gin.Context, target fishVoiceTarget, method, path string, body io.Reader) (*http.Response, error) {
	targetURL := target.baseURL + path
	query := c.Request.URL.Query()
	// `model` is a gateway-only channel routing parameter
	query.Del("model")
	if encoded := query.Encode(); encoded != "" {
		targetURL += "?" + encoded
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), method, targetURL, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		// only meaningful when this call forwards the client's body; a request we
		// issue ourselves with no body must not inherit the client's length
		req.ContentLength = c.Request.ContentLength
		if contentType := c.Request.Header.Get("Content-Type"); contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
	}
	req.Header.Set("Authorization", "Bearer "+target.apiKey)

	client := service.GetHttpClient()
	if target.proxy != "" {
		proxyClient, proxyErr := service.NewProxyHttpClient(target.proxy)
		if proxyErr != nil {
			return nil, proxyErr
		}
		client = proxyClient
	}
	return client.Do(req)
}

func copyFishAudioResponse(c *gin.Context, resp *http.Response, body []byte) {
	for k, v := range resp.Header {
		if !service.ShouldCopyUpstreamHeader(c, k, v) {
			continue
		}
		c.Writer.Header().Set(k, v[0])
	}
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err := c.Writer.Write(body); err != nil {
		logger.LogError(c, fmt.Sprintf("failed to write fish audio response: %v", err))
	}
}

func voiceNotFound() *types.NewAPIError {
	// Deliberately identical for "does not exist" and "belongs to somebody
	// else" so the endpoint cannot be used to enumerate other users' voices.
	return types.NewErrorWithStatusCode(
		errors.New("voice model not found"),
		types.ErrorCodeInvalidRequest,
		http.StatusNotFound,
		types.ErrOptionWithSkipRetry(),
	)
}

// FishAudioCreateVoice proxies voice model creation and records ownership.
func FishAudioCreateVoice(c *gin.Context) *types.NewAPIError {
	userId := c.GetInt("id")
	target, targetErr := resolveTargetFromContext(c)
	if targetErr != nil {
		return types.NewError(targetErr, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	resp, err := doFishAudioRequest(c, target, http.MethodPost, "/model", c.Request.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// A voice this gateway cannot record is a voice nobody can ever list,
		// update or delete, sitting on an account every user shares. Rather than
		// leave one behind, undo the creation upstream and report the failure.
		var created fishaudio.VoiceModel
		if unmarshalErr := common.Unmarshal(responseBody, &created); unmarshalErr != nil || created.Id == "" {
			logger.LogError(c, fmt.Sprintf("fish audio create voice response carries no id: %s", common.LocalLogPreview(string(responseBody))))
			discardUpstreamVoice(c, target, created.Id)
			return types.NewError(
				errors.New("voice model was created upstream but its id could not be read, so it could not be recorded"),
				types.ErrorCodeBadResponse,
				types.ErrOptionWithSkipRetry(),
			)
		}
		voice := &model.FishVoice{
			UserId:     userId,
			ChannelId:  target.channelId,
			UpstreamId: created.Id,
			Title:      created.Title,
			State:      created.State,
			Visibility: created.Visibility,
		}
		if insertErr := voice.Insert(); insertErr != nil {
			logger.LogError(c, fmt.Sprintf(
				"fish audio voice %s created on channel #%d for user %d but ownership record failed: %v",
				created.Id, target.channelId, userId, insertErr))
			discardUpstreamVoice(c, target, created.Id)
			return types.NewError(insertErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}

	copyFishAudioResponse(c, resp, responseBody)
	return nil
}

// discardUpstreamVoice removes a voice this gateway created but failed to take
// ownership of. Best effort: if it does not succeed the id is logged so the
// leftover can be cleared by hand from the shared account.
func discardUpstreamVoice(c *gin.Context, target fishVoiceTarget, upstreamId string) {
	if upstreamId == "" {
		return
	}
	resp, err := doFishAudioRequest(c, target, http.MethodDelete, "/model/"+upstreamId, nil)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to discard unrecorded fish audio voice %s on channel #%d: %v", upstreamId, target.channelId, err))
		return
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.LogError(c, fmt.Sprintf("failed to discard unrecorded fish audio voice %s on channel #%d: upstream returned %d", upstreamId, target.channelId, resp.StatusCode))
	}
}

// FishAudioListVoices answers from the local ownership table. The upstream
// listing spans the whole shared account and must never be proxied through.
func FishAudioListVoices(c *gin.Context) *types.NewAPIError {
	userId := c.GetInt("id")
	voices, err := model.GetFishVoicesByUser(userId)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError)
	}
	items := make([]gin.H, 0, len(voices))
	for _, voice := range voices {
		items = append(items, gin.H{
			"_id":        voice.UpstreamId,
			"title":      voice.Title,
			"state":      voice.State,
			"visibility": voice.Visibility,
			"created_at": voice.CreatedTime,
		})
	}
	c.JSON(http.StatusOK, gin.H{"total": len(items), "items": items})
	return nil
}

// FishAudioVoiceById proxies get / update / delete for a single voice after
// verifying the caller owns it, routing back to its original channel.
func FishAudioVoiceById(c *gin.Context) *types.NewAPIError {
	userId := c.GetInt("id")
	upstreamId := strings.TrimSpace(c.Param("id"))

	voice, err := model.GetFishVoiceByUpstreamId(userId, upstreamId)
	if err != nil {
		if errors.Is(err, model.ErrFishVoiceNotFound) {
			return voiceNotFound()
		}
		return types.NewError(err, types.ErrorCodeQueryDataError)
	}

	target, targetErr := resolveTargetFromChannel(voice.ChannelId)
	if targetErr != nil {
		return types.NewError(targetErr, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	resp, reqErr := doFishAudioRequest(c, target, c.Request.Method, "/model/"+upstreamId, c.Request.Body)
	if reqErr != nil {
		return types.NewOpenAIError(reqErr, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		switch c.Request.Method {
		case http.MethodDelete:
			if deleteErr := model.DeleteFishVoice(userId, upstreamId); deleteErr != nil {
				logger.LogError(c, fmt.Sprintf("failed to delete fish audio voice record %s: %v", upstreamId, deleteErr))
			}
		case http.MethodPatch:
			var updated fishaudio.VoiceModel
			if unmarshalErr := common.Unmarshal(responseBody, &updated); unmarshalErr == nil {
				if updateErr := voice.UpdateMeta(updated.Title, updated.Visibility, updated.State); updateErr != nil {
					logger.LogError(c, fmt.Sprintf("failed to update fish audio voice record %s: %v", upstreamId, updateErr))
				}
			}
		}
	}

	copyFishAudioResponse(c, resp, responseBody)
	return nil
}
