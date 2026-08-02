package constant

import "github.com/QuantumNous/new-api/relaykit/types"

// Fish Audio model sets moved to relaykit/types so the audio DTO's token
// counting has no host imports; aliases keep host code compiling unchanged.
var FishAudioTTSModels = types.FishAudioTTSModels

// IsByteBilledTTSModel reports whether a model is billed per UTF-8 byte.
// Channel-level model aliases that do not match fall back to rune-based
// pre-consume; settlement still bills bytes, so they merely under-reserve.
func IsByteBilledTTSModel(modelName string) bool {
	return types.IsByteBilledTTSModel(modelName)
}

// FishAudioProtectedTTSFields are the /v1/tts body fields a client must not set
// through `metadata`. `text` is the quantity billing counts, so letting metadata
// replace it would synthesise one body of text while charging for another. Every
// other provider field stays passthrough, including `reference_id`, which
// multi-speaker synthesis passes as an array of voice ids.
var FishAudioProtectedTTSFields = []string{"text"}
