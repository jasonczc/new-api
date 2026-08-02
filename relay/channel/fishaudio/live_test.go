package fishaudio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// stubBilling records Reserve targets and fails once the cap is exceeded,
// standing in for a user whose balance runs out mid-session.
type stubBilling struct {
	cap      int
	reserved []int
}

func (s *stubBilling) Settle(actualQuota int) error { return nil }
func (s *stubBilling) Refund(c *gin.Context)        {}
func (s *stubBilling) NeedsRefund() bool            { return false }
func (s *stubBilling) GetPreConsumedQuota() int     { return 0 }
func (s *stubBilling) Reserve(targetQuota int) error {
	if targetQuota > s.cap {
		return errors.New("insufficient quota")
	}
	s.reserved = append(s.reserved, targetQuota)
	return nil
}

var _ relaycommon.BillingSettler = (*stubBilling)(nil)

func TestProjectLiveQuota(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{}
	info.PriceData = hosttypes.PriceData{ModelRatio: 7.5}
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	// 1000 bytes at ratio 7.5 -> 7500 quota == $0.015, matching $15 / 1M bytes
	assert.Equal(t, 7500, projectLiveQuota(info, 1000))
	// group ratio participates
	info.PriceData.GroupRatioInfo.GroupRatio = 0.5
	assert.Equal(t, 3750, projectLiveQuota(info, 1000))
	// free tier: ratio 0 must not trigger reservations
	info.PriceData.ModelRatio = 0
	assert.Zero(t, projectLiveQuota(info, 1000))
}

func mustMsgpack(t *testing.T, v any) []byte {
	t.Helper()
	data, err := msgpack.Marshal(v)
	require.NoError(t, err, "msgpack.Marshal")
	return data
}

// TestHandleTTSLiveStopsWhenQuotaExhausted drives handleTTSLive over real
// websocket connections and asserts that text the user cannot pay for is never
// forwarded upstream — i.e. the audio is not produced and then billed.
func TestHandleTTSLiveStopsWhenQuotaExhausted(t *testing.T) {
	upgrader := websocket.Upgrader{}

	var upstreamMu sync.Mutex
	upstreamFrames := 0
	upstreamReady := make(chan struct{})
	// Lets the test wait until a forwarded frame has actually landed upstream
	// before sending the next one, instead of racing the teardown that follows
	// the quota cut-off.
	upstreamGotFrame := make(chan struct{}, 4)
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		close(upstreamReady)
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				return
			}
			upstreamMu.Lock()
			upstreamFrames++
			upstreamMu.Unlock()
			select {
			case upstreamGotFrame <- struct{}{}:
			default:
			}
		}
	}))
	defer upstreamSrv.Close()

	// The gate reserves reserveStepQuota ahead, so it only re-reserves once
	// consumption catches up. First frame: 100 bytes -> 750 projected -> reserves
	// 5750. Second frame: 800 bytes total -> 6000 projected > 5750 reserved ->
	// asks for 11000, which the cap refuses.
	billing := &stubBilling{cap: 5750}
	var usageResult atomic.Value
	handlerDone := make(chan struct{})

	clientSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		targetConn, _, dialErr := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(upstreamSrv.URL, "http"), nil)
		if !assert.NoError(t, dialErr, "dial upstream") {
			return
		}
		<-upstreamReady

		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/tts/live", nil)

		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
		info.ClientWs = clientConn
		info.TargetWs = targetConn
		info.Billing = billing
		info.PriceData = hosttypes.PriceData{ModelRatio: 7.5}
		info.PriceData.GroupRatioInfo.GroupRatio = 1

		usage, apiErr := handleTTSLive(c, info)
		if !assert.Nil(t, apiErr, "handleTTSLive") {
			return
		}
		usageResult.Store(usage)
	}))
	defer clientSrv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(clientSrv.URL, "http"), nil)
	require.NoError(t, err, "dial gateway")
	defer conn.Close()

	// 100 bytes: affordable
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, mustMsgpack(t, map[string]any{
		"event": "text", "text": strings.Repeat("a", 100),
	})), "write first frame")
	select {
	case <-upstreamGotFrame:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the affordable frame was never forwarded upstream")
	}
	// 700 more bytes: pushes the projection past what the user can reserve
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, mustMsgpack(t, map[string]any{
		"event": "text", "text": strings.Repeat("b", 700),
	})), "write second frame")

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "handleTTSLive did not return after quota exhaustion")
	}

	upstreamMu.Lock()
	forwarded := upstreamFrames
	upstreamMu.Unlock()
	require.Equal(t, 1, forwarded, "the unpaid frame must not be forwarded upstream")

	usage, ok := usageResult.Load().(*dto.RealtimeUsage)
	require.True(t, ok, "usage not recorded")
	assert.Equal(t, 100, usage.InputTokens, "only the forwarded frame is billed")
	assert.Len(t, billing.reserved, 1, "want exactly 1 successful reservation, got %v", billing.reserved)
}

func TestLiveEventTextBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		frame []byte
		want  int
	}{
		{
			name: "text event counts UTF-8 bytes",
			frame: mustMsgpack(t, map[string]any{
				"event": "text",
				"text":  "你好", // 2 runes, 6 bytes
			}),
			want: 6,
		},
		{
			name: "start event counts initial request text",
			frame: mustMsgpack(t, map[string]any{
				"event": "start",
				"request": map[string]any{
					"text":         "hello",
					"format":       "mp3",
					"chunk_length": 300,
				},
			}),
			want: 5,
		},
		{
			name: "start event without text",
			frame: mustMsgpack(t, map[string]any{
				"event": "start",
				"request": map[string]any{
					"format": "opus",
				},
			}),
			want: 0,
		},
		{
			name:  "flush event carries no text",
			frame: mustMsgpack(t, map[string]any{"event": "flush"}),
			want:  0,
		},
		{
			name:  "stop event carries no text",
			frame: mustMsgpack(t, map[string]any{"event": "stop"}),
			want:  0,
		},
		{
			name:  "undecodable frame counts zero",
			frame: []byte{0xc1, 0xff, 0x00},
			want:  0,
		},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, decodeLiveClientEvent(tc.frame).textBytes(), tc.name)
	}
}
