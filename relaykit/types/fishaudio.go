package types

import "strings"

// FishAudioTTSModels are the Fish Audio speech models billed per UTF-8 byte of
// input text ($15 / 1M bytes). Both pre-consume estimation and settlement must
// count bytes, not runes — CJK text differs by 3x. Lives in relaykit so the
// audio DTO's token counting has no host imports; constant keeps aliases.
// https://docs.fish.audio/developer-guide/models-pricing/pricing-and-rate-limits
var FishAudioTTSModels = map[string]bool{
	"s1":            true,
	"s2-pro":        true,
	"s2.1-pro":      true,
	"s2.1-pro-free": true,
}

// FishAudioLiveModels are the models the streaming TTS WebSocket accepts.
// https://docs.fish.audio/api-reference/endpoint/websocket/tts-live
var FishAudioLiveModels = map[string]bool{
	"s1":     true,
	"s2-pro": true,
}

// IsByteBilledTTSModel reports whether a model is billed per UTF-8 byte.
// Channel-level model aliases that do not match fall back to rune-based
// pre-consume; settlement still bills bytes, so they merely under-reserve.
func IsByteBilledTTSModel(modelName string) bool {
	return FishAudioTTSModels[strings.TrimSpace(modelName)]
}
