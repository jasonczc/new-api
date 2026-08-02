package fishaudio

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	ResponseFormat string
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseUrl := info.ChannelBaseUrl
	if baseUrl == "" {
		baseUrl = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeFishAudio]
	}
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		return fmt.Sprintf("%s/v1/tts", baseUrl), nil
	case relayconstant.RelayModeAudioTranscription:
		return fmt.Sprintf("%s/v1/asr", baseUrl), nil
	default:
		return "", fmt.Errorf("unsupported relay mode: %d", info.RelayMode)
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		// Fish Audio selects the model via the `model` header, not the body
		req.Set("model", info.UpstreamModelName)
	}
	return nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		// The relay mode is decided by path, not by the client's model name, so a
		// caller can POST /v1/audio/speech naming the ASR model (ratio 3) and have
		// byte-metered TTS settled at that cheaper rate. Pin synthesis to the TTS
		// models, the same way the ASR path only ever serves transcribe-1.
		if !channelconstant.IsByteBilledTTSModel(info.UpstreamModelName) {
			return nil, badRequest("fish audio speech supports only s1, s2-pro, s2.1-pro and s2.1-pro-free; got %q", info.UpstreamModelName)
		}
		return convertTTSRequest(request)
	case relayconstant.RelayModeAudioTranscription:
		a.ResponseFormat = request.ResponseFormat
		return convertASRRequest(c, request)
	default:
		return nil, errors.New("unsupported audio relay mode")
	}
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("fish audio does not support chat completions")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == relayconstant.RelayModeAudioTranscription {
		return channel.DoFormRequest(a, c, info, requestBody)
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		return handleTTSResponse(c, resp, info)
	case relayconstant.RelayModeAudioTranscription:
		return handleASRResponse(c, resp, info, a.ResponseFormat)
	default:
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported relay mode: %d", info.RelayMode),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
		)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
