package fishaudio

import (
	"math"
	"testing"

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

	t.Run("refuses a duration that is unusable on its face", func(t *testing.T) {
		// these fall back to the gateway-measured file duration
		cases := map[string]float64{
			"zero":              0,
			"negative":          -1,
			"NaN":               math.NaN(),
			"positive infinity": math.Inf(1),
			"negative infinity": math.Inf(-1),
		}
		for name, duration := range cases {
			t.Run(name, func(t *testing.T) {
				_, usable := asrPromptTokens(duration)
				require.False(t, usable, "must not bill on %v", duration)
			})
		}
	})

	t.Run("clamps an implausibly large duration to the cap rather than discarding it", func(t *testing.T) {
		// billing a full day of audio is far safer than falling back to the
		// client-forgeable file measurement or, worse, to zero
		capTokens := maxBillableASRSeconds / 60 * 1000
		for name, duration := range map[string]float64{
			"exactly the cap": maxBillableASRSeconds,
			"just over":       maxBillableASRSeconds + 1,
			"absurdly large":  1e15,
		} {
			t.Run(name, func(t *testing.T) {
				got, usable := asrPromptTokens(duration)
				require.True(t, usable, "must clamp and bill %v", duration)
				assert.Equal(t, capTokens, got)
			})
		}
	})
}
