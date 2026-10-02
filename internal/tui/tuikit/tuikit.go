// Package tuikit holds small pieces shared by Nexus's Bubble Tea apps.
package tuikit

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nothikemu/nexus/internal/ui"
)

// TickMsg drives animation and refresh loops.
type TickMsg time.Time

// Tick schedules a TickMsg.
func Tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return TickMsg(t) })
}

// Spark is the twinkle frame for animation step n.
func Spark(t *ui.Theme, n int) string {
	frames := []string{"·", "✧", "✦", "✦", "✧", "·"}
	if !t.Mode.Unicode {
		return "*"
	}
	return t.Primary.Bold(true).Render(frames[n%len(frames)])
}

// Hints renders key hints: "↑↓ rows · enter open · q quit". Arguments
// alternate key, action.
func Hints(t *ui.Theme, pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, t.Secondary.Render(pairs[i])+" "+t.Muted.Render(pairs[i+1]))
	}
	return strings.Join(parts, "   ")
}

// FitLines pads or trims s to exactly h lines (for fixed-height views).
func FitLines(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// Clip truncates every line of s to w cells.
func Clip(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ui.Truncate(l, w, "…")
	}
	return strings.Join(lines, "\n")
}
