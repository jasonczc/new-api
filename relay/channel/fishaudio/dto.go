package fishaudio

// The TTS request body is built as a map in tts.go rather than a struct, so
// that provider fields arriving via `metadata` are never silently dropped.
// Known /v1/tts body fields, for reference:
//
//	text, reference_id (string or []string), references (inline cloning),
//	format, sample_rate, mp3_bitrate, opus_bitrate, latency, chunk_length,
//	min_chunk_length, prosody{speed,volume,normalize_loudness}, normalize,
//	temperature, top_p, repetition_penalty, max_new_tokens,
//	condition_on_previous_chunks, early_stop_threshold, features
//
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/text-to-speech

// ASRResponse is the Fish Audio POST /v1/asr response body.
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/speech-to-text
type ASRResponse struct {
	Text     string       `json:"text"`
	Duration float64      `json:"duration"` // seconds of processed audio, billing basis
	Segments []ASRSegment `json:"segments,omitempty"`
}

type ASRSegment struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// VoiceModel is the subset of the Fish Audio /model response this gateway
// records for ownership tracking.
// https://docs.fish.audio/api-reference/endpoint/model/create-model
type VoiceModel struct {
	Id         string `json:"_id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Visibility string `json:"visibility"`
}
