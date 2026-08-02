package fishaudio

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func convertASRRequest(c *gin.Context, request dto.AudioRequest) (io.Reader, error) {
	formData, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, badRequest("multipart form is invalid: %v", err)
	}

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Fish Audio /v1/asr only understands audio / language / ignore_timestamps;
	// OpenAI-only fields (model, prompt, temperature, ...) must not be forwarded.
	for _, key := range []string{"language", "ignore_timestamps"} {
		for _, value := range formData.Value[key] {
			if err := writer.WriteField(key, value); err != nil {
				return nil, fmt.Errorf("error writing form field %s: %w", key, err)
			}
		}
	}
	if needsTimestamps(request.ResponseFormat) && len(formData.Value["ignore_timestamps"]) == 0 {
		if err := writer.WriteField("ignore_timestamps", "false"); err != nil {
			return nil, fmt.Errorf("error writing form field ignore_timestamps: %w", err)
		}
	}

	fileHeaders := formData.File["file"]
	if len(fileHeaders) == 0 {
		return nil, badRequest("file is required")
	}
	fileHeader := fileHeaders[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening audio file: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("audio", fileHeader.Filename)
	if err != nil {
		return nil, fmt.Errorf("error creating form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("error copying audio file: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("error closing multipart writer: %w", err)
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return &requestBody, nil
}

// maxBillableASRSeconds is the point past which a reported audio duration stops
// being plausible. Fish Audio tells us how much audio it billed us for, but that
// is still an untrusted number arriving over the network, and it gets multiplied
// into a charge against the user — so a malformed or absurd value must not be
// taken at face value. A day of audio is far beyond any single upload.
const maxBillableASRSeconds = 24 * 60 * 60

// asrPromptTokens converts a reported audio duration into the repo-wide
// 1 minute = 1000 pseudo-token convention. It reports false only when the
// duration is unusable on its face — negative, zero, or non-finite — so the
// caller falls back to the duration this gateway measured from the uploaded
// file. An implausibly large but finite duration is clamped to the cap instead
// of refused, so it bills a bounded maximum rather than the forgeable fallback.
func asrPromptTokens(duration float64) (int, bool) {
	if math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return 0, false
	}
	// An implausibly long duration is clamped to the cap rather than discarded:
	// billing a full day of audio is far safer than falling back to the
	// client-measurable file duration (which an attacker forges short) or, when
	// token counting is disabled, to zero.
	if duration > maxBillableASRSeconds {
		duration = maxBillableASRSeconds
	}
	return common.QuotaRound(math.Ceil(duration) / 60.0 * 1000), true
}

func handleASRResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, responseFormat string) (usage any, err *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var fishResp ASRResponse
	if unmarshalErr := common.Unmarshal(responseBody, &fishResp); unmarshalErr != nil {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("failed to unmarshal fish audio ASR response: %w", unmarshalErr),
			types.ErrorCodeBadResponseBody,
			http.StatusInternalServerError,
		)
	}

	switch responseFormat {
	case "text":
		c.String(http.StatusOK, fishResp.Text)
	case "srt":
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(renderSRT(fishResp)))
	case "vtt":
		c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(renderVTT(fishResp)))
	case "verbose_json":
		verbose := dto.WhisperVerboseJSONResponse{
			Task:     "transcribe",
			Duration: fishResp.Duration,
			Text:     fishResp.Text,
		}
		for i, segment := range fishResp.Segments {
			verbose.Segments = append(verbose.Segments, dto.Segment{
				Id:    i,
				Start: segment.Start,
				End:   segment.End,
				Text:  segment.Text,
			})
		}
		data, marshalErr := common.Marshal(verbose)
		if marshalErr != nil {
			return nil, types.NewOpenAIError(marshalErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		c.Data(http.StatusOK, "application/json", data)
	default:
		data, marshalErr := common.Marshal(dto.AudioResponse{Text: fishResp.Text})
		if marshalErr != nil {
			return nil, types.NewOpenAIError(marshalErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		c.Data(http.StatusOK, "application/json", data)
	}

	// Fish Audio bills ASR per second of audio rounded up; settle on the
	// upstream-reported duration using the repo-wide 1 minute = 1000 pseudo-token
	// convention so ratio 3 lands on $0.36 / audio hour.
	promptTokens, usable := asrPromptTokens(fishResp.Duration)
	switch {
	case !usable:
		logger.LogError(c, fmt.Sprintf(
			"fish audio asr reported an unusable duration (%v); billing the duration measured from the uploaded file instead",
			fishResp.Duration))
	case fishResp.Duration > maxBillableASRSeconds:
		// clamped, not refused: bill the cap and leave a request-correlated audit
		// line so an implausible upstream duration is visible after the fact
		logger.LogWarn(c, fmt.Sprintf(
			"fish audio asr reported an implausible duration (%v); clamped to %d seconds for billing",
			fishResp.Duration, maxBillableASRSeconds))
	}
	if promptTokens <= 0 {
		promptTokens = info.GetEstimatePromptTokens()
	}
	usageObj := &dto.Usage{
		PromptTokens: promptTokens,
		TotalTokens:  promptTokens,
	}
	return usageObj, nil
}
