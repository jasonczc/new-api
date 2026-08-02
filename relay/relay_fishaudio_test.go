package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newVoiceContext(t *testing.T, channelType int, baseUrl string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/fishaudio/model", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
	common.SetContextKey(c, constant.ContextKeyChannelId, 7)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-channel-secret")
	if baseUrl != "" {
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseUrl)
	}
	return c
}

// Voice management bypasses the adaptor layer, so nothing else stops a channel
// of another provider — picked because it happened to list the routing model —
// from receiving its own key at api.fish.audio.
func TestResolveTargetFromContextRejectsNonFishAudioChannel(t *testing.T) {
	cases := map[string]int{
		"vertex, whose key is a service account JSON": constant.ChannelTypeVertexAi,
		"aws, whose key carries ak|sk|region":         constant.ChannelTypeAws,
		"a plain openai channel":                      constant.ChannelTypeOpenAI,
		"no channel selected at all":                  0,
	}
	for name, channelType := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := resolveTargetFromContext(newVoiceContext(t, channelType, ""))
			require.Error(t, err, "a non fish audio channel must not be used for voice management")
		})
	}
}

func TestResolveTargetFromContextAcceptsFishAudioChannel(t *testing.T) {
	target, err := resolveTargetFromContext(newVoiceContext(t, constant.ChannelTypeFishAudio, "https://proxy.example.com"))
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example.com", target.baseURL)
	assert.Equal(t, "sk-channel-secret", target.apiKey)
	assert.Equal(t, 7, target.channelId)
}

func TestResolveTargetFromContextFallsBackToDefaultBaseURL(t *testing.T) {
	target, err := resolveTargetFromContext(newVoiceContext(t, constant.ChannelTypeFishAudio, ""))
	require.NoError(t, err)
	assert.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeFishAudio], target.baseURL)
}
