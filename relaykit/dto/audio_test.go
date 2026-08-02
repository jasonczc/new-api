package dto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestAudioRequestTokenCountMetaBillsFishAudioByBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		request   AudioRequest
		wantType  types.TokenType
		wantText  string
		wantBytes int // expected count under the chosen token type, 0 to skip
	}{
		{
			name:      "fish audio CJK counts UTF-8 bytes",
			request:   AudioRequest{Model: "s1", Input: "你好世界"},
			wantType:  types.TokenTypeUTF8Bytes,
			wantText:  "你好世界",
			wantBytes: 12, // 4 runes, 12 bytes — pre-consume must match settlement
		},
		{
			name:     "fish audio free tier also bills bytes",
			request:  AudioRequest{Model: "s2.1-pro-free", Input: "hi"},
			wantType: types.TokenTypeUTF8Bytes,
			wantText: "hi",
		},
		{
			name:     "openai tts keeps rune counting",
			request:  AudioRequest{Model: "tts-1", Input: "你好世界"},
			wantType: types.TokenTypeTextNumber,
			wantText: "你好世界",
		},
		{
			name:     "gpt models keep the tokenizer",
			request:  AudioRequest{Model: "gpt-4o-mini-tts", Input: "hello"},
			wantType: types.TokenTypeTokenizer,
			wantText: "hello",
		},
		{
			name:     "voice design falls back to instruction text",
			request:  AudioRequest{Model: "voice-design-1", Instruction: "warm narrator"},
			wantType: types.TokenTypeTextNumber,
			wantText: "warm narrator",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := tc.request.GetTokenCountMeta()
			require.Equal(t, tc.wantType, meta.TokenType)
			require.Equal(t, tc.wantText, meta.CombineText)
			if tc.wantBytes > 0 {
				require.Len(t, meta.CombineText, tc.wantBytes)
			}
		})
	}
}
