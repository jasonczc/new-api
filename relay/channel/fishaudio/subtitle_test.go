package fishaudio

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTimestamp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		seconds   float64
		separator string
		want      string
	}{
		{0, ",", "00:00:00,000"},
		{1.234, ",", "00:00:01,234"},
		{61.5, ".", "00:01:01.500"},
		{3725.75, ",", "01:02:05,750"},
		{-1, ",", "00:00:00,000"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, formatTimestamp(tc.seconds, tc.separator), "formatTimestamp(%v)", tc.seconds)
	}
}

func TestRenderSRT(t *testing.T) {
	t.Parallel()

	resp := ASRResponse{
		Text:     "hello world",
		Duration: 5,
		Segments: []ASRSegment{
			{Text: " hello", Start: 0, End: 1.5},
			{Text: "world", Start: 1.5, End: 5},
		},
	}

	want := "1\n00:00:00,000 --> 00:00:01,500\nhello\n\n2\n00:00:01,500 --> 00:00:05,000\nworld\n\n"
	require.Equal(t, want, renderSRT(resp))
}

func TestRenderVTT(t *testing.T) {
	t.Parallel()

	resp := ASRResponse{
		Text:     "hello",
		Duration: 2,
		Segments: []ASRSegment{{Text: "hello", Start: 0, End: 2}},
	}

	got := renderVTT(resp)
	require.True(t, strings.HasPrefix(got, "WEBVTT\n\n"), "renderVTT() must start with the WEBVTT header, got %q", got)
	assert.Contains(t, got, "00:00:00.000 --> 00:00:02.000", "want dot millisecond separator")
	assert.NotContains(t, got, ",000", "renderVTT() must not use SRT comma separators")
}

func TestSubtitleFallbackWhenNoSegments(t *testing.T) {
	t.Parallel()

	// Fish Audio omits segments when ignore_timestamps is on; a single cue
	// spanning the recording is better than an empty file.
	resp := ASRResponse{Text: "one long take", Duration: 12.25}
	got := renderSRT(resp)
	assert.Contains(t, got, "one long take")
	assert.Contains(t, got, "00:00:00,000 --> 00:00:12,250")

	assert.Empty(t, renderSRT(ASRResponse{}), "empty transcription should render no cues")
	assert.Equal(t, "WEBVTT\n\n", renderVTT(ASRResponse{}), "empty transcription should render only the VTT header")
}

func TestNeedsTimestamps(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"verbose_json", "srt", "vtt"} {
		assert.True(t, needsTimestamps(format), "needsTimestamps(%q)", format)
	}
	for _, format := range []string{"", "json", "text"} {
		assert.False(t, needsTimestamps(format), "needsTimestamps(%q)", format)
	}
}
