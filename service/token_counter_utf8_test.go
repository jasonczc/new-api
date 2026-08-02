package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	constant2 "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fish Audio bills TTS per UTF-8 byte, so pre-consume must reserve on the same
// basis settlement charges. This pins the TokenTypeUTF8Bytes branch of the
// estimator against the rune-counting default: for CJK text the two differ by
// 3x, which is exactly the under-reservation a regression here would cause.
func TestEstimateRequestTokenUTF8BytesVsRunes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// EstimateRequestToken short-circuits to 0 when token counting is disabled;
	// force it on for this test and restore it afterwards.
	original := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = original })

	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
		return c
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIAudio,
		RelayMode:   constant2.RelayModeAudioSpeech,
	}

	const cjk = "你好世界" // 4 runes, 12 UTF-8 bytes

	bytesTokens, err := EstimateRequestToken(newContext(), &types.TokenCountMeta{
		CombineText: cjk,
		TokenType:   types.TokenTypeUTF8Bytes,
	}, info)
	require.NoError(t, err)
	assert.Equal(t, 12, bytesTokens, "byte-billed TTS must reserve the UTF-8 byte count")

	runeTokens, err := EstimateRequestToken(newContext(), &types.TokenCountMeta{
		CombineText: cjk,
		TokenType:   types.TokenTypeTextNumber,
	}, info)
	require.NoError(t, err)
	assert.Equal(t, 4, runeTokens, "the default path still counts runes")
}
