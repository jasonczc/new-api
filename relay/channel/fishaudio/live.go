package fishaudio

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

// reserveStepQuota is how far ahead of current consumption the session keeps
// its reservation, so a long stream does not hit the database on every frame.
// 5000 quota is $0.01 — roughly 1.3KB of text at the default TTS ratio.
const reserveStepQuota = 5000

// liveClientEvent mirrors the client-side events of the Fish Audio streaming
// TTS protocol (msgpack): start / text / flush / stop.
// https://docs.fish.audio/api-reference/endpoint/websocket/tts-live
type liveClientEvent struct {
	Event   string `msgpack:"event"`
	Text    string `msgpack:"text"`
	Request *struct {
		Text        string `msgpack:"text"`
		ReferenceId string `msgpack:"reference_id"`
	} `msgpack:"request"`
}

// decodeLiveClientEvent parses one client msgpack frame. Frames this gateway
// cannot decode yield a zero event: they carry no text to meter and no voice to
// authorise, and Fish Audio remains free to reject them itself.
func decodeLiveClientEvent(data []byte) liveClientEvent {
	var event liveClientEvent
	if err := msgpack.Unmarshal(data, &event); err != nil {
		return liveClientEvent{}
	}
	return event
}

// textBytes returns the UTF-8 byte count of the text this frame submits, which
// is Fish Audio's billing basis for streaming TTS.
func (e liveClientEvent) textBytes() int {
	switch e.Event {
	case "text":
		return len(e.Text)
	case "start":
		if e.Request != nil {
			return len(e.Request.Text)
		}
	}
	return 0
}

// voiceId returns the cloned voice the session asks to speak in, which the
// protocol carries on the opening frame only.
func (e liveClientEvent) voiceId() string {
	if e.Event == "start" && e.Request != nil {
		return e.Request.ReferenceId
	}
	return ""
}

// closeLiveSession tells the client why the gateway is ending the session,
// before the pump returns and both connections are torn down.
func closeLiveSession(conn *websocket.Conn, reason string) {
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason),
		time.Now().Add(time.Second),
	)
}

// projectLiveQuota estimates the quota owed for a byte count under the simple
// per-token formula. It only has to preserve ordering — settlement remains
// authoritative and may use a billing expression instead.
//
// The result saturates rather than wrapping. A projection that overflowed into
// a negative would read as "less than already reserved", so the gate below would
// stop asking for quota and let the rest of the session stream unbilled.
func projectLiveQuota(info *relaycommon.RelayInfo, bytes int) int {
	ratio := info.PriceData.ModelRatio * info.PriceData.GroupRatioInfo.GroupRatio
	if ratio <= 0 || bytes <= 0 {
		return 0
	}
	return common.QuotaFromFloat(math.Ceil(float64(bytes) * ratio))
}

// handleTTSLive pumps msgpack frames between the client and Fish Audio in both
// directions, accumulating the UTF-8 byte count of all submitted text, which is
// Fish Audio's billing basis for streaming TTS.
//
// Text frames are metered *before* being forwarded: a session that outruns the
// user's balance is cut off rather than delivered and billed afterwards.
func handleTTSLive(c *gin.Context, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	clientConn := info.ClientWs
	targetConn := info.TargetWs
	if clientConn == nil || targetConn == nil {
		return nil, types.NewError(errors.New("websocket connection not established"), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}

	var inputBytes atomic.Int64
	var quotaExceeded atomic.Bool
	done := make(chan struct{}, 2)

	gopool.Go(func() {
		defer func() { done <- struct{}{} }()
		reserved := 0
		for {
			messageType, data, readErr := clientConn.ReadMessage()
			if readErr != nil {
				return
			}

			event := decodeLiveClientEvent(data)

			// the shared upstream key owns every voice this gateway cloned and
			// cannot tell whose is whose, so another user's voice is refused here
			// exactly as the synthesis and management endpoints refuse it
			if voice := event.voiceId(); voice != "" {
				ownedByOther, ownErr := model.IsFishVoiceOwnedByOther(info.UserId, voice)
				if ownErr != nil {
					logger.LogError(c, fmt.Sprintf("fish audio live tts could not verify ownership of voice model %s: %v", voice, ownErr))
				}
				if ownErr != nil || ownedByOther {
					closeLiveSession(clientConn, "voice model not found")
					return
				}
			}

			// meter before forwarding: the audio must not be produced unless the
			// user can pay for the text that triggers it
			frameBytes := event.textBytes()
			if frameBytes > 0 {
				total := int(inputBytes.Add(int64(frameBytes)))
				if info.Billing != nil {
					if projected := projectLiveQuota(info, total); projected > reserved {
						target := projected + reserveStepQuota
						if reserveErr := info.Billing.Reserve(target); reserveErr != nil {
							quotaExceeded.Store(true)
							logger.LogError(c, fmt.Sprintf("fish audio live tts stopped, insufficient quota: %v", reserveErr))
							closeLiveSession(clientConn, "insufficient quota")
							// roll back the unpaid bytes so settlement bills only
							// what was actually forwarded
							inputBytes.Add(int64(-frameBytes))
							return
						}
						reserved = target
					}
				}
			}

			if writeErr := targetConn.WriteMessage(messageType, data); writeErr != nil {
				// the frame never reached Fish Audio, so settlement must not bill
				// it either
				inputBytes.Add(int64(-frameBytes))
				return
			}
		}
	})

	gopool.Go(func() {
		defer func() { done <- struct{}{} }()
		for {
			messageType, data, readErr := targetConn.ReadMessage()
			if readErr != nil {
				return
			}
			info.SetFirstResponseTime()
			if writeErr := clientConn.WriteMessage(messageType, data); writeErr != nil {
				return
			}
		}
	})

	// the first side to close ends the session; closing both conns unblocks the
	// other pump goroutine
	<-done
	_ = targetConn.Close()
	_ = clientConn.Close()
	<-done

	if quotaExceeded.Load() {
		logger.LogWarn(c, "fish audio live tts session terminated due to insufficient quota")
	}

	total := int(inputBytes.Load())
	realtimeUsage := &dto.RealtimeUsage{}
	realtimeUsage.InputTokens = total
	realtimeUsage.InputTokenDetails.TextTokens = total
	realtimeUsage.TotalTokens = total
	return realtimeUsage, nil
}
