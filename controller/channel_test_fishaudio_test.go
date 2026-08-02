package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/require"
)

func TestBuildTestRequestForAudioSpeech(t *testing.T) {
	request := buildTestRequest("s1", string(constant.EndpointTypeAudioSpeech), nil, false)

	audioRequest, ok := request.(*dto.AudioRequest)
	require.True(t, ok, "speech endpoint must produce an AudioRequest, not a chat request")
	require.Equal(t, "s1", audioRequest.Model)
	require.NotEmpty(t, audioRequest.Input)
	require.Equal(t, "mp3", audioRequest.ResponseFormat)
}

func TestTestChannelRejectsTranscriptionEndpoint(t *testing.T) {
	channel := &model.Channel{Id: 1, Type: constant.ChannelTypeFishAudio}

	result := testChannel(context.Background(), channel, 1, "transcribe-1", string(constant.EndpointTypeAudioTranscription), false)

	require.Error(t, result.localErr, "transcription cannot be tested without an audio upload")
	require.Contains(t, result.localErr.Error(), "audio file upload")
}

func TestFishAudioChannelTestPrefersFreeModel(t *testing.T) {
	// A Fish Audio channel is probed with speech synthesis; the model must be a
	// byte-billed TTS model so the probe hits /v1/tts rather than a chat route.
	require.True(t, constant.IsByteBilledTTSModel("s2.1-pro-free"))
	require.True(t, constant.IsByteBilledTTSModel("s1"))
	require.False(t, constant.IsByteBilledTTSModel("transcribe-1"))
	require.False(t, constant.IsByteBilledTTSModel("voice-design-1"))
}
