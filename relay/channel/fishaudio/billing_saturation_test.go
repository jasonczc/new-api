package fishaudio

import (
	"math"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The audio duration Fish Audio reports is an untrusted number off the network
// that gets multiplied into a charge, so it has to be bounded before it becomes
// a token count.
func TestASRPromptTokens(t *testing.T) {
	t.Parallel()

	t.Run("bills the reported duration when it is plausible", func(t *testing.T) {
		cases := map[float64]int{
			60:   1000, // one minute
			30:   500,
			90:   1500,
			0.4:  17, // rounded up to a whole second first
			3600: 60000,
		}
		for duration, want := range cases {
			got, usable := asrPromptTokens(duration)
			require.True(t, usable, "duration %v should be billable", duration)
			assert.Equal(t, want, got, "duration %v", duration)
		}
	})

	t.Run("refuses a duration it cannot bill on", func(t *testing.T) {
		cases := map[string]float64{
			"zero":              0,
			"negative":          -1,
			"NaN":               math.NaN(),
			"positive infinity": math.Inf(1),
			"negative infinity": math.Inf(-1),
			"absurdly large":    1e15,
			"just over the cap": maxBillableASRSeconds + 1,
		}
		for name, duration := range cases {
			t.Run(name, func(t *testing.T) {
				got, usable := asrPromptTokens(duration)
				require.False(t, usable, "must not bill on %v", duration)
				assert.Zero(t, got)
			})
		}
	})
}

// A projection that wrapped negative would read as "less than already
// reserved", so the live gate would stop asking for quota and let the rest of
// the session stream unbilled.
func TestProjectLiveQuotaSaturates(t *testing.T) {
	t.Parallel()

	newInfo := func(modelRatio, groupRatio float64) *relaycommon.RelayInfo {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
		info.PriceData = types.PriceData{ModelRatio: modelRatio}
		info.PriceData.GroupRatioInfo.GroupRatio = groupRatio
		return info
	}

	assert.Equal(t, 750, projectLiveQuota(newInfo(7.5, 1), 100))
	assert.Equal(t, 1500, projectLiveQuota(newInfo(7.5, 2), 100))

	t.Run("no quota owed when the model is free", func(t *testing.T) {
		assert.Zero(t, projectLiveQuota(newInfo(0, 1), 100))
	})

	t.Run("saturates instead of wrapping negative", func(t *testing.T) {
		// common.QuotaFromFloat clamps at int32 max because quota columns are
		// 32-bit; the gate then keeps demanding quota until pre-consume fails.
		got := projectLiveQuota(newInfo(math.MaxFloat64, 1), math.MaxInt)
		assert.Equal(t, math.MaxInt32, got)
		assert.Positive(t, got, "an overflowed projection would disable the billing gate")
	})
}
