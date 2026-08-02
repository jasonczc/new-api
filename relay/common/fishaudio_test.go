package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// Fish Audio has no chat completions endpoint at all, so the OpenAI
// stream_options parameter never applies to it.
func TestFishAudioDoesNotEnableOpenAIStreamOptions(t *testing.T) {
	require.False(t, streamSupportedChannels[constant.ChannelTypeFishAudio])
}
