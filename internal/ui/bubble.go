package ui

import "strings"

// Bubble draws a rounded speech bubble whose tail points left, for putting
// words next to the mascot:
//
//	╭──────────────────────╮
//	│ good morning.        │
//	┤ my-app is awake.     │
//	╰──────────────────────╯
//
// tailRow is the line (0 = top border) where the tail attaches. In plain
// mode it returns the text, prefixed with the speaker's name when given.
func (t *Theme) Bubble(text string, maxWidth, tailRow int) string {
	if !t.Mode.Unicode || t.Mode.Plain {
		return text
	}
	if maxWidth < 16 {
		maxWidth = 16
	}
	lines := strings.Split(Wrap(text, maxWidth-4), "\n")
	inner := 0
	for _, l := range lines {
		inner = max(inner, Width(l))
	}
	border := t.Faint
	var b strings.Builder
	b.WriteString(border.Render("╭" + strings.Repeat("─", inner+2) + "╮"))
	for i, l := range lines {
		left := border.Render("│")
		if i+1 == tailRow {
			left = border.Render("┤")
		}
		b.WriteString("\n" + left + " " + t.Text.Render(PadRight(l, inner)) + " " + border.Render("│"))
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", inner+2)+"╯"))
	return b.String()
}

// Speak lays a bubble beside a sprite, joined by a short tail.
func (t *Theme) Speak(sprite, text string, maxWidth int) string {
	if sprite == "" {
		return text
	}
	bubble := t.Bubble(text, maxWidth, 2)
	h := strings.Count(sprite, "\n") + 1
	bh := strings.Count(bubble, "\n") + 1
	// Vertically align the bubble's tail with the sprite's face (row 2).
	pad := 0
	if bh < h {
		pad = (h - bh) / 2
	}
	tail := strings.Repeat("\n", pad+2) + t.Faint.Render("─")
	return Columns(0, sprite, " "+tail, strings.Repeat("\n", pad)+bubble)
}
