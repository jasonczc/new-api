package fishaudio

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURL(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	cases := []struct {
		relayMode int
		want      string
	}{
		{relayconstant.RelayModeAudioSpeech, "https://api.fish.audio/v1/tts"},
		{relayconstant.RelayModeAudioTranscription, "https://api.fish.audio/v1/asr"},
	}
	for _, tc := range cases {
		info := &relaycommon.RelayInfo{
			RelayMode: tc.relayMode,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelBaseUrl: "https://api.fish.audio",
			},
		}
		got, err := adaptor.GetRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}

	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeAudioTranslation,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	_, err := adaptor.GetRequestURL(info)
	require.Error(t, err, "GetRequestURL should reject audio translation")
}

func TestConvertTTSRequest(t *testing.T) {
	t.Parallel()

	speed := 1.5
	request := dto.AudioRequest{
		Model:          "s1",
		Input:          "你好，Fish Audio",
		Voice:          "9a9cf47702da476aa4629e2506d4a857",
		ResponseFormat: "wav",
		Speed:          &speed,
		Metadata:       json.RawMessage(`{"latency":"low","chunk_length":200}`),
	}

	reader, err := convertTTSRequest(testUserId, request)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, request.Input, payload["text"])
	assert.Equal(t, request.Voice, payload["reference_id"])
	assert.Equal(t, "wav", payload["format"])
	assert.Equal(t, "low", payload["latency"])
	assert.Equal(t, float64(200), payload["chunk_length"])
	prosody, ok := payload["prosody"].(map[string]any)
	require.True(t, ok, "prosody = %#v, want object", payload["prosody"])
	assert.Equal(t, speed, prosody["speed"])
	assert.NotContains(t, payload, "model", "model must not be sent in the body, it goes to the header")
}

func TestConvertTTSRequestMetadataReferenceArray(t *testing.T) {
	t.Parallel()

	request := dto.AudioRequest{
		Model:    "s2-pro",
		Input:    "<|speaker:0|>Hi<|speaker:1|>Hello",
		Metadata: json.RawMessage(`{"reference_id":["speaker-a","speaker-b"]}`),
	}

	reader, err := convertTTSRequest(testUserId, request)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload struct {
		ReferenceID []string `json:"reference_id"`
		Format      string   `json:"format"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, []string{"speaker-a", "speaker-b"}, payload.ReferenceID)
	assert.Equal(t, "mp3", payload.Format, "want default mp3")
}

func TestConvertTTSRequestMetadataPassesUnknownFields(t *testing.T) {
	t.Parallel()

	// Fields this gateway does not model must still reach upstream — a struct
	// round-trip would silently drop them.
	request := dto.AudioRequest{
		Model: "s2.1-pro",
		Input: "hi",
		Metadata: json.RawMessage(`{
			"references":[{"audio":"base64data","text":"sample"}],
			"normalize":false,
			"mp3_bitrate":192,
			"repetition_penalty":1.2,
			"condition_on_previous_chunks":true,
			"features":{"experimental":"on"}
		}`),
	}

	reader, err := convertTTSRequest(testUserId, request)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))

	assert.Contains(t, payload, "references", "references must pass through for inline voice cloning")
	// explicit false / zero values must survive too
	assert.Equal(t, false, payload["normalize"], "want explicit false")
	assert.Equal(t, float64(192), payload["mp3_bitrate"])
	assert.Equal(t, 1.2, payload["repetition_penalty"])
	assert.Equal(t, true, payload["condition_on_previous_chunks"])
	assert.IsType(t, map[string]any{}, payload["features"], "features must stay a nested object")
	assert.NotContains(t, payload, "model", "model must never appear in the body")
}

func TestConvertTTSRequestRejectsSSE(t *testing.T) {
	t.Parallel()

	request := dto.AudioRequest{Model: "s1", Input: "hi", StreamFormat: "sse"}
	_, err := convertTTSRequest(testUserId, request)
	require.Error(t, err, "convertTTSRequest should reject stream_format sse")
}

func TestConvertTTSRequestUnsupportedFormat(t *testing.T) {
	t.Parallel()

	request := dto.AudioRequest{Model: "s1", Input: "hi", ResponseFormat: "flac"}
	_, err := convertTTSRequest(testUserId, request)
	require.Error(t, err, "convertTTSRequest should reject flac")
}

func TestHandleTTSResponseUsageCountsUTF8Bytes(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)

	input := "你好世界" // 4 runes, 12 UTF-8 bytes
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{},
		Request:     &dto.AudioRequest{Model: "s1", Input: input},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(strings.NewReader("fake-audio-bytes")),
	}

	usage, apiErr := handleTTSResponse(c, resp, info)
	require.Nil(t, apiErr)

	usageObj, ok := usage.(*dto.Usage)
	require.True(t, ok, "usage type = %T, want *dto.Usage", usage)
	assert.Equal(t, len(input), usageObj.PromptTokens, "UTF-8 bytes, not runes")
	assert.Zero(t, usageObj.CompletionTokenDetails.AudioTokens, "AudioTokens must stay 0 so billing takes the text path")
	assert.Zero(t, usageObj.PromptTokensDetails.AudioTokens, "AudioTokens must stay 0 so billing takes the text path")
	assert.Equal(t, "fake-audio-bytes", recorder.Body.String(), "audio body must pass through")
}

func TestHandleASRResponseSubtitleFormats(t *testing.T) {
	t.Parallel()

	body := `{"text":"hello world","duration":5,"segments":[{"text":"hello","start":0,"end":1.5},{"text":"world","start":1.5,"end":5}]}`

	for _, tc := range []struct {
		format      string
		wantContent []string
		rejectJSON  bool
	}{
		{format: "srt", wantContent: []string{"1\n00:00:00,000 --> 00:00:01,500\nhello", "2\n"}, rejectJSON: true},
		{format: "vtt", wantContent: []string{"WEBVTT", "00:00:01.500 --> 00:00:05.000\nworld"}, rejectJSON: true},
		{format: "text", wantContent: []string{"hello world"}, rejectJSON: true},
	} {
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)

		info := &relaycommon.RelayInfo{
			RelayMode:   relayconstant.RelayModeAudioTranscription,
			ChannelMeta: &relaycommon.ChannelMeta{},
		}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}

		usage, apiErr := handleASRResponse(c, resp, info, tc.format)
		require.Nil(t, apiErr, "format %s", tc.format)
		got := recorder.Body.String()
		for _, want := range tc.wantContent {
			assert.Contains(t, got, want, "format %s", tc.format)
		}
		if tc.rejectJSON {
			assert.False(t, strings.HasPrefix(strings.TrimSpace(got), "{"), "%s must not fall back to JSON, got %q", tc.format, got)
		}
		// ceil(5)/60*1000 = 83.33 -> 83
		require.IsType(t, &dto.Usage{}, usage)
		assert.Equal(t, 83, usage.(*dto.Usage).PromptTokens, "format %s", tc.format)
	}
}

func TestHandleASRResponse(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeAudioTranscription,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"text":"hello world","duration":90.5,"segments":[{"text":"hello","start":0,"end":1.2}]}`,
		)),
	}

	usage, apiErr := handleASRResponse(c, resp, info, "verbose_json")
	require.Nil(t, apiErr)

	usageObj := usage.(*dto.Usage)
	// ceil(90.5)/60*1000 = 1516.67 -> 1517
	assert.Equal(t, 1517, usageObj.PromptTokens)

	var verbose dto.WhisperVerboseJSONResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &verbose))
	assert.Equal(t, "hello world", verbose.Text)
	assert.Equal(t, 90.5, verbose.Duration)
	require.Len(t, verbose.Segments, 1)
	assert.Equal(t, 1.2, verbose.Segments[0].End)
}
