package fishaudio

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertVoice records a voice as belonging to a user, the way a successful
// POST /fishaudio/model would.
func insertVoice(t *testing.T, userId int, upstreamId string) {
	t.Helper()
	voice := &model.FishVoice{UserId: userId, ChannelId: 1, UpstreamId: upstreamId}
	require.NoError(t, voice.Insert())
	t.Cleanup(func() {
		model.DB.Where("upstream_id = ?", upstreamId).Delete(&model.FishVoice{})
	})
}

// The billed quantity and the synthesised quantity have to be the same string.
// Billing counts request.Input, so metadata must not be able to put a different
// body of text on the wire.
func TestConvertTTSRequestRejectsMetadataTextOverride(t *testing.T) {
	request := dto.AudioRequest{
		Model:    "s1",
		Input:    "a",
		Metadata: json.RawMessage(`{"text":"一段完全不同且长得多的文本"}`),
	}

	_, err := convertTTSRequest(testUserId, request)
	require.Error(t, err, "a metadata text override must be rejected")
	assert.Contains(t, err.Error(), "text")
}

// Empty input settles to zero quota, so it must not reach upstream at all.
func TestConvertTTSRequestRejectsEmptyInput(t *testing.T) {
	_, err := convertTTSRequest(testUserId, dto.AudioRequest{Model: "s1", Input: ""})
	require.Error(t, err)
}

// One upstream key is shared by every gateway user, so upstream cannot tell
// whose cloned voice is whose — the ownership table is the only thing stopping
// a caller speaking in somebody else's voice.
func TestConvertTTSRequestRejectsAnotherUsersVoice(t *testing.T) {
	insertVoice(t, testUserId+1, "voice-owned-by-someone-else")

	request := dto.AudioRequest{
		Model: "s1",
		Input: "hi",
		Voice: "voice-owned-by-someone-else",
	}

	_, err := convertTTSRequest(testUserId, request)
	require.Error(t, err, "another user's voice must be rejected")
}

// Multi-speaker synthesis passes an array of ids through metadata; every one of
// them has to be authorised, not just the OpenAI `voice` field.
func TestConvertTTSRequestRejectsAnotherUsersVoiceInMetadataArray(t *testing.T) {
	insertVoice(t, testUserId, "voice-mine")
	insertVoice(t, testUserId+1, "voice-theirs")

	request := dto.AudioRequest{
		Model:    "s1",
		Input:    "<|speaker:0|>Hi<|speaker:1|>Hello",
		Metadata: json.RawMessage(`{"reference_id":["voice-mine","voice-theirs"]}`),
	}

	_, err := convertTTSRequest(testUserId, request)
	require.Error(t, err, "a multi-speaker array containing another user's voice must be rejected")
}

// Voices this gateway did not create are not ours to gate: Fish Audio's public
// catalogue and the caller's own clones both have to keep working.
func TestConvertTTSRequestAllowsOwnAndUnknownVoices(t *testing.T) {
	insertVoice(t, testUserId, "voice-mine-allowed")

	cases := []struct {
		name  string
		voice string
	}{
		{"own voice", "voice-mine-allowed"},
		{"public catalogue voice this gateway never recorded", "802e3bc2b27e49c2995d23ef70e6ac89"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader, err := convertTTSRequest(testUserId, dto.AudioRequest{Model: "s1", Input: "hi", Voice: tc.voice})
			require.NoError(t, err)

			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(body, &payload))
			assert.Equal(t, tc.voice, payload["reference_id"])
		})
	}
}

func TestVoiceIdsFrom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input any
		want  []string
	}{
		{"single id", "voice-a", []string{"voice-a"}},
		{"empty string", "", nil},
		{"multi speaker array", []any{"voice-a", "voice-b"}, []string{"voice-a", "voice-b"}},
		{"array with non-strings", []any{"voice-a", 42, ""}, []string{"voice-a"}},
		{"absent", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, voiceIdsFrom(tc.input))
		})
	}
}
