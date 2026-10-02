package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nothikemu/nexus/internal/textutil"
)

// Width is the display width of s in terminal cells, ignoring ANSI escapes.
func Width(s string) int { return lipgloss.Width(s) }

// Truncate shortens s to at most w cells, appending an ellipsis when cut.
func Truncate(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, ellipsis)
}

// PadRight pads s with spaces to exactly w cells (truncating if needed).
func PadRight(s string, w int) string {
	s = Truncate(s, w, "…")
	if gap := w - Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// PadLeft right-aligns s within w cells.
func PadLeft(s string, w int) string {
	s = Truncate(s, w, "…")
	if gap := w - Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// Center centers s within w cells.
func Center(s string, w int) string {
	gap := w - Width(s)
	if gap <= 0 {
		return s
	}
	left := gap / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", gap-left)
}

// Indent prefixes every non-empty line of s with n spaces.
func Indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// Wrap soft-wraps plain text to w cells on word boundaries.
func Wrap(s string, w int) string {
	if w <= 0 {
		return s
	}
	return ansi.Wordwrap(s, w, "")
}

// Plural returns "1 table" / "3 tables".
func Plural(n int64, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%s %s", Count(n), plural)
}

// Count formats an integer with thousands separators: 184,291.
func Count(n int64) string { return textutil.Thousands(n) }

// Compact formats large numbers briefly: 1.2k, 3.4M.
func Compact(n float64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1e9:
		return trimZero(fmt.Sprintf("%.1f", n/1e9)) + "B"
	case abs >= 1e6:
		return trimZero(fmt.Sprintf("%.1f", n/1e6)) + "M"
	case abs >= 1e3:
		return trimZero(fmt.Sprintf("%.1f", n/1e3)) + "k"
	case abs >= 10 || abs == 0:
		return fmt.Sprintf("%.0f", n)
	default:
		return trimZero(fmt.Sprintf("%.1f", n))
	}
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

// Bytes formats a byte count: 2.8 MB.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return trimZero(fmt.Sprintf("%.1f", float64(n)/float64(div))) + " " + string("KMGTPE"[exp]) + "B"
}

// Duration formats a duration for humans at the precision that matters.
func Duration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// Millis formats a float of milliseconds (as reported by EXPLAIN).
func Millis(ms float64) string {
	return Duration(time.Duration(ms * float64(time.Millisecond)))
}

// Ago formats a timestamp relative to now: "3m ago".
func Ago(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < 5*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	default:
		return t.Format("2006-01-02")
	}
}

// Percent formats a 0..1 ratio as a percentage.
func Percent(f float64) string {
	if f >= 0.9995 && f < 1 {
		return "99.9%"
	}
	return trimZero(fmt.Sprintf("%.1f", f*100)) + "%"
}

// JoinSep joins non-empty parts with the theme's separator: "14 tables · 2.8 MB".
func (t *Theme) JoinSep(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, t.Faint.Render(" "+t.Glyphs.Sep+" "))
}

// Rule draws a horizontal rule w cells wide.
func (t *Theme) Rule(w int) string {
	if w <= 0 {
		return ""
	}
	return t.Faint.Render(strings.Repeat(t.Glyphs.Rule, w))
}
