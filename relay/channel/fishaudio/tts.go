package fishaudio

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// badRequest reports a mistake in the client's request. A plain error returned
// from ConvertAudioRequest surfaces as HTTP 500, because types.NewError defaults
// to it, which would tell the caller to retry something that can only ever fail.
// types.NewError preserves a typed error passed up from here, so the status set
// below is the one the client sees.
func badRequest(format string, args ...any) error {
	return types.NewErrorWithStatusCode(
		fmt.Errorf(format, args...),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

func mapTTSFormat(responseFormat string) (string, error) {
	switch responseFormat {
	case "":
		return "mp3", nil
	case "mp3", "opus", "wav", "pcm":
		return responseFormat, nil
	default:
		return "", badRequest("fish audio does not support response_format %q (supported: mp3, opus, wav, pcm)", responseFormat)
	}
}

// voiceIdsFrom extracts the cloned voices a /v1/tts body asks to speak in.
// Fish Audio accepts either a single id or, for multi-speaker synthesis, an
// array of them.
func voiceIdsFrom(referenceId any) []string {
	switch value := referenceId.(type) {
	case string:
		if value != "" {
			return []string{value}
		}
	case []any:
		ids := make([]string, 0, len(value))
		for _, item := range value {
			if id, ok := item.(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	return nil
}

// ensureVoicesUsable refuses cloned voices that belong to another gateway user.
// Every Fish Audio channel shares one upstream API key, so upstream happily
// synthesises with any voice that key owns — the ownership recorded in
// model.FishVoice is the only thing keeping one user's cloned voice from being
// spoken by another.
//
// It runs over the final request body rather than the OpenAI `voice` field so
// that ids arriving through `metadata`, including a multi-speaker array, are
// authorised too.
func ensureVoicesUsable(userId int, referenceId any) error {
	for _, voice := range voiceIdsFrom(referenceId) {
		ownedByOther, err := model.IsFishVoiceOwnedByOther(userId, voice)
		if err != nil {
			return types.NewError(err, types.ErrorCodeQueryDataError)
		}
		if ownedByOther {
			// Worded and statused exactly as the management endpoints answer for
			// somebody else's voice, so synthesis cannot be used to tell an id
			// that exists from one that does not.
			return types.NewErrorWithStatusCode(
				fmt.Errorf("voice model %q not found", voice),
				types.ErrorCodeInvalidRequest,
				http.StatusNotFound,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}
	return nil
}

// protectedFieldHint names the OpenAI field that owns a body field `metadata`
// is not allowed to set.
var protectedFieldHint = map[string]string{
	"text": "input",
}

// convertTTSRequest builds the Fish Audio /v1/tts body as a map so that every
// provider field passed through `metadata` reaches upstream — including ones
// this gateway does not model (normalize, mp3_bitrate, opus_bitrate,
// repetition_penalty, min_chunk_length, condition_on_previous_chunks,
// early_stop_threshold, features, ...). Decoding into a struct would silently
// drop them.
//
// The fields in FishAudioProtectedTTSFields are the exception: this gateway
// owns them, so `metadata` may not set them. `text` is the quantity billing
// counts — letting it diverge from `input` would mean synthesising one body of
// text while charging for another — and the model travels in the `model` HTTP
// header, never in the body.
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/text-to-speech
func convertTTSRequest(userId int, request dto.AudioRequest) (io.Reader, error) {
	if request.StreamFormat == "sse" {
		// Fish Audio streams raw audio bytes, not OpenAI-style SSE audio events;
		// accepting sse here would emit audio under a text/event-stream header
		return nil, badRequest("fish audio does not support stream_format \"sse\"; omit it to receive a chunked audio stream")
	}
	if request.Input == "" {
		// Nothing to synthesise, and an empty input settles to no quota at all
		return nil, badRequest("input is required and must not be empty")
	}
	format, err := mapTTSFormat(request.ResponseFormat)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"text":   request.Input,
		"format": format,
	}
	if request.Voice != "" {
		payload["reference_id"] = request.Voice
	}
	if request.Speed != nil {
		payload["prosody"] = map[string]any{"speed": *request.Speed}
	}

	if len(request.Metadata) > 0 {
		var overrides map[string]any
		if err := common.Unmarshal(request.Metadata, &overrides); err != nil {
			return nil, badRequest("metadata is not a JSON object: %v", err)
		}
		for _, field := range channelconstant.FishAudioProtectedTTSFields {
			if _, present := overrides[field]; present {
				return nil, badRequest("metadata may not set %q; use %q instead", field, protectedFieldHint[field])
			}
		}
		for key, value := range overrides {
			payload[key] = value
		}
	}
	delete(payload, "model")

	// authorise the voices the final body asks for, so an id smuggled in through
	// metadata is checked just like one given as `voice`
	if err := ensureVoicesUsable(userId, payload["reference_id"]); err != nil {
		return nil, err
	}

	jsonData, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshalling fish audio request: %w", err)
	}
	return bytes.NewReader(jsonData), nil
}

func handleTTSResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	usageObj := &dto.Usage{}
	// Fish Audio bills TTS by UTF-8 bytes of the input text, so usage must be
	// reported in bytes (len), not runes — for CJK text they differ by 3x.
	if audioReq, ok := info.Request.(*dto.AudioRequest); ok && audioReq.Input != "" {
		inputBytes := len(audioReq.Input)
		usageObj.PromptTokens = inputBytes
		usageObj.PromptTokensDetails.TextTokens = inputBytes
		usageObj.TotalTokens = inputBytes
	} else {
		usageObj.PromptTokens = info.GetEstimatePromptTokens()
		usageObj.TotalTokens = usageObj.PromptTokens
	}

	for k, v := range resp.Header {
		if !service.ShouldCopyUpstreamHeader(c, k, v) {
			continue
		}
		c.Writer.Header().Set(k, v[0])
	}
	c.Writer.WriteHeader(resp.StatusCode)
	c.Writer.WriteHeaderNow()

	buf := make([]byte, 16*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				logger.LogError(c, fmt.Sprintf("failed to write fish audio TTS response: %v", writeErr))
				break
			}
			c.Writer.Flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				logger.LogError(c, fmt.Sprintf("failed to read fish audio TTS response: %v", readErr))
			}
			break
		}
	}

	return usageObj, nil
}
