package dto

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
)

type AudioRequest struct {
	Model          string          `json:"model"`
	Input          string          `json:"input"`
	Voice          string          `json:"voice"`
	Instructions   string          `json:"instructions,omitempty"`
	Instruction    string          `json:"instruction,omitempty"` // fish audio voice design prompt
	ResponseFormat string          `json:"response_format,omitempty"`
	Speed          *float64        `json:"speed,omitempty"`
	StreamFormat   string          `json:"stream_format,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	// vllm-omini
	TaskType                json.RawMessage `json:"task_type,omitempty"`
	Language                json.RawMessage `json:"language,omitempty"`
	RefAudio                json.RawMessage `json:"ref_audio,omitempty"`
	RefText                 json.RawMessage `json:"ref_text,omitempty"`
	XVectorOnlyMode         json.RawMessage `json:"x_vector_only_mode,omitempty"`
	MaxNewTokens            json.RawMessage `json:"max_new_tokens,omitempty"`
	InitialCodecChunkFrames json.RawMessage `json:"initial_codec_chunk_frames,omitempty"`
	// TODO：ensure that the logic remains correct after the stream is started.
	//Stream                  json.RawMessage `json:"stream,omitempty"`
}

func (r *AudioRequest) GetTokenCountMeta() *types.TokenCountMeta {
	combineText := r.Input
	if combineText == "" {
		// voice design carries its prompt in `instruction`; include it so the
		// sensitive-word check sees it
		combineText = r.Instruction
	}
	meta := &types.TokenCountMeta{
		CombineText: combineText,
		TokenType:   types.TokenTypeTextNumber,
	}
	if types.IsByteBilledTTSModel(r.Model) {
		// Fish Audio bills per UTF-8 byte; reserve on the same basis we settle on
		meta.TokenType = types.TokenTypeUTF8Bytes
	} else if strings.Contains(r.Model, "gpt") {
		meta.TokenType = types.TokenTypeTokenizer
	}
	return meta
}

func (r *AudioRequest) IsStream(c *http.Request) bool {
	return r.StreamFormat == "sse"
}

func (r *AudioRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

type AudioResponse struct {
	Text string `json:"text"`
}

type WhisperVerboseJSONResponse struct {
	Task     string    `json:"task,omitempty"`
	Language string    `json:"language,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Text     string    `json:"text,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

type Segment struct {
	Id               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens"`
	Temperature      float64 `json:"temperature"`
	AvgLogprob       float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
}
