package ui

import (
	"math"
	"strings"
)

var sparkTicks = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders a series as a row of block heights. The most recent value
// is on the right. Width pads/trims to a fixed number of cells.
func (t *Theme) Sparkline(series []float64, width int, hex string) string {
	if width <= 0 {
		width = len(series)
	}
	if len(series) > width {
		series = series[len(series)-width:]
	}
	if !t.Mode.Unicode {
		return asciiSpark(series, width)
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range series {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	if lo > 0 {
		lo = 0 // anchor to zero so flat-but-busy series don't look empty
	}
	var b strings.Builder
	for _, v := range series {
		idx := 0
		if hi > lo {
			idx = int(math.Round((v - lo) / (hi - lo) * float64(len(sparkTicks)-1)))
		}
		if v > 0 && idx == 0 {
			idx = 1 // any activity is visible
		}
		b.WriteRune(sparkTicks[idx])
	}
	if hex == "" {
		hex = t.Palette.Secondary
	}
	// History not yet collected shows as a faint baseline.
	return t.Faint.Render(strings.Repeat("▁", width-len(series))) + t.Style(hex).Render(b.String())
}

func asciiSpark(series []float64, width int) string {
	ticks := []rune("_.-=#")
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range series {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	if lo > 0 {
		lo = 0
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", width-len(series)))
	for _, v := range series {
		idx := 0
		if hi > lo {
			idx = int(math.Round((v - lo) / (hi - lo) * float64(len(ticks)-1)))
		}
		b.WriteRune(ticks[idx])
	}
	return b.String()
}

// Bar renders a horizontal proportion bar of width cells:
// ██████████░░░░░ with eighth-block precision on the leading edge.
func (t *Theme) Bar(fraction float64, width int, hex string) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(fraction) || fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	if !t.Mode.Unicode {
		full := int(math.Round(fraction * float64(width)))
		return "[" + strings.Repeat("#", full) + strings.Repeat(".", width-full) + "]"
	}
	eighths := []rune(" ▏▎▍▌▋▊▉")
	total := fraction * float64(width)
	full := int(total)
	rem := int((total - float64(full)) * 8)
	var b strings.Builder
	b.WriteString(strings.Repeat("█", full))
	used := full
	if full < width && rem > 0 {
		b.WriteRune(eighths[rem])
		used++
	}
	if hex == "" {
		hex = t.Palette.Primary
	}
	return t.Style(hex).Render(b.String()) + t.Faint.Render(strings.Repeat("░", width-used))
}
