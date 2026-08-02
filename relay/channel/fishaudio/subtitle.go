package fishaudio

import (
	"fmt"
	"strings"
	"time"
)

// needsTimestamps reports whether a response format requires segment timings,
// which Fish Audio only returns when ignore_timestamps is false.
func needsTimestamps(responseFormat string) bool {
	switch responseFormat {
	case "verbose_json", "srt", "vtt":
		return true
	default:
		return false
	}
}

// formatTimestamp renders seconds as HH:MM:SS followed by a separator and
// milliseconds — "," for SRT, "." for WebVTT.
func formatTimestamp(seconds float64, msSeparator string) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds * float64(time.Second))
	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	millis := int((d % time.Second) / time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", hours, minutes, secs, msSeparator, millis)
}

// subtitleCues returns the segments to render, falling back to a single cue
// spanning the whole recording when the upstream returned no segmentation.
func subtitleCues(resp ASRResponse) []ASRSegment {
	if len(resp.Segments) > 0 {
		return resp.Segments
	}
	if strings.TrimSpace(resp.Text) == "" {
		return nil
	}
	return []ASRSegment{{Text: resp.Text, Start: 0, End: resp.Duration}}
}

func renderSRT(resp ASRResponse) string {
	var b strings.Builder
	for i, cue := range subtitleCues(resp) {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n",
			i+1,
			formatTimestamp(cue.Start, ","),
			formatTimestamp(cue.End, ","),
			strings.TrimSpace(cue.Text),
		)
	}
	return b.String()
}

func renderVTT(resp ASRResponse) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, cue := range subtitleCues(resp) {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n",
			formatTimestamp(cue.Start, "."),
			formatTimestamp(cue.End, "."),
			strings.TrimSpace(cue.Text),
		)
	}
	return b.String()
}
