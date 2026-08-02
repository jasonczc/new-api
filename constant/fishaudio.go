package constant

import "github.com/QuantumNous/new-api/relaykit/types"

// Fish Audio model sets moved to relaykit/types so the audio DTO's token
// counting has no host imports; aliases keep host code compiling unchanged.
var (
	FishAudioTTSModels  = types.FishAudioTTSModels
	FishAudioLiveModels = types.FishAudioLiveModels
)

// IsByteBilledTTSModel reports whether a model is billed per UTF-8 byte.
// Channel-level model aliases that do not match fall back to rune-based
// pre-consume; settlement still bills bytes, so they merely under-reserve.
func IsByteBilledTTSModel(modelName string) bool {
	return types.IsByteBilledTTSModel(modelName)
}

// FishAudioVoiceDesignModel is the only model /v1/voice-design serves, and it
// is priced per successful request rather than per token. The route decides
// the operation, so the model is pinned to the route instead of being read
// from the client — otherwise a caller could name a per-token speech model and
// pay a speech rate for a voice design call.
const FishAudioVoiceDesignModel = "voice-design-1"

// FishAudioDefaultRoutingModel steers channel selection for calls that need a
// Fish Audio channel but no particular model, such as voice management.
const FishAudioDefaultRoutingModel = "s1"

// FishAudioProtectedTTSFields are the /v1/tts body fields a client must not set
// through `metadata`. `text` is the quantity billing counts, so letting metadata
// replace it would synthesise one body of text while charging for another.
//
// Every other provider field stays passthrough. `reference_id` in particular is
// allowed, because multi-speaker synthesis legitimately passes an array of voice
// ids that way — those ids are authorised against the ownership table rather
// than refused outright.
var FishAudioProtectedTTSFields = []string{"text"}
