package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestFishAudioChannelTypeUsesFishAudioAPIType(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeFishAudio)
	require.True(t, ok)
	require.Equal(t, constant.APITypeFishAudio, apiType)
}

// Fish Audio serves no chat endpoint, so every model resolves to one of the two
// audio endpoints rather than falling through to the chat default.
func TestFishAudioEndpointTypesAreAudioOnly(t *testing.T) {
	cases := map[string]constant.EndpointType{
		"s1":            constant.EndpointTypeAudioSpeech,
		"s2-pro":        constant.EndpointTypeAudioSpeech,
		"s2.1-pro":      constant.EndpointTypeAudioSpeech,
		"s2.1-pro-free": constant.EndpointTypeAudioSpeech,
		"transcribe-1":  constant.EndpointTypeAudioTranscription,
	}
	for modelName, want := range cases {
		require.Equal(t, []constant.EndpointType{want},
			GetEndpointTypesByChannelType(constant.ChannelTypeFishAudio, modelName),
			"unexpected endpoint type for %s", modelName)
	}
}
