package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Panel is a rounded, titled box:
//
//	╭─ my-app · local ─────────────────────╮
//	│                                      │
//	│   Database     ● running             │
//	│                                      │
//	╰──────────────────────────────────────╯
//
// In plain mode it degrades to a title line followed by the indented body.
type Panel struct {
	Title    string // shown in the top border
	Right    string // optional right-aligned text in the top border (e.g. a badge)
	Body     string
	Width    int // total width including borders; 0 = fit content (min 40)
	Padding  int // horizontal padding inside the border (default 2)
	VPadding int // blank lines above and below the body (0 = default 1, -1 = none)
	Accent   string
}

// Render draws the panel.
func (t *Theme) Panel(p Panel) string {
	padX := p.Padding
	if padX == 0 {
		padX = 2
	}
	padY := p.VPadding
	switch {
	case padY == 0:
		padY = 1
	case padY < 0:
		padY = 0
	}

	lines := strings.Split(strings.TrimRight(p.Body, "\n"), "\n")

	if !t.Mode.Unicode || t.Mode.Plain {
		var b strings.Builder
		head := strings.ToUpper(p.Title)
		if p.Right != "" {
			head += "  " + p.Right
		}
		if head != "" {
			b.WriteString(head)
			b.WriteString("\n")
		}
		for _, l := range lines {
			b.WriteString(Indent(l, 2))
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}

	inner := 0
	for _, l := range lines {
		if w := Width(l); w > inner {
			inner = w
		}
	}
	inner += padX * 2
	titleW := Width(p.Title) + Width(p.Right) + 8
	if inner < titleW {
		inner = titleW
	}
	if p.Width > 0 {
		inner = p.Width - 2
	} else if inner < 38 {
		inner = 38
	}
	if max := t.Mode.ContentWidth() - 2; inner > max && p.Width == 0 {
		inner = max
	}

	border := t.Faint
	if p.Accent != "" {
		border = t.Style(p.Accent)
	}
	b := lipgloss.RoundedBorder()

	// Top border with embedded title and optional right text.
	var top strings.Builder
	top.WriteString(border.Render(b.TopLeft + b.Top))
	used := 1
	if p.Title != "" {
		title := " " + p.Title + " "
		top.WriteString(t.Title.Render(title))
		used += Width(title)
	}
	right := ""
	if p.Right != "" {
		right = " " + p.Right + " "
	}
	fill := inner - used - Width(right) - 1
	if fill < 1 {
		fill = 1
	}
	top.WriteString(border.Render(strings.Repeat(b.Top, fill)))
	if right != "" {
		top.WriteString(right)
		top.WriteString(border.Render(b.Top))
	}
	top.WriteString(border.Render(b.TopRight))

	side := border.Render(b.Left)
	sideR := border.Render(b.Right)
	blank := side + strings.Repeat(" ", inner) + sideR

	var out strings.Builder
	out.WriteString(top.String())
	out.WriteString("\n")
	for i := 0; i < padY; i++ {
		out.WriteString(blank + "\n")
	}
	pad := strings.Repeat(" ", padX)
	for _, l := range lines {
		out.WriteString(side + pad + PadRight(l, inner-padX*2) + pad + sideR + "\n")
	}
	for i := 0; i < padY; i++ {
		out.WriteString(blank + "\n")
	}
	out.WriteString(border.Render(b.BottomLeft + strings.Repeat(b.Bottom, inner) + b.BottomRight))
	return out.String()
}

// Section renders an uppercase heading over an indented body, used inside
// panels and reports:
//
//	DATABASE
//	├─ 24 tables
func (t *Theme) Section(title, body string) string {
	head := t.Heading.Render(strings.ToUpper(title))
	if body == "" {
		return head
	}
	return head + "\n" + body
}

// Badge renders a small inline label: ● ONLINE.
func (t *Theme) Badge(text string, style lipgloss.Style) string {
	if t.Mode.Plain {
		return "[" + text + "]"
	}
	return style.Bold(true).Render(text)
}

// Dot renders a status dot followed by a label.
func (t *Theme) Dot(on bool, style lipgloss.Style, label string) string {
	g := t.Glyphs.Dot
	if !on {
		g = t.Glyphs.DotOff
	}
	s := style.Render(g)
	if label == "" {
		return s
	}
	return s + " " + label
}

// Columns lays out blocks side by side with a gap, aligning their tops.
func Columns(gap int, blocks ...string) string {
	split := make([][]string, len(blocks))
	widths := make([]int, len(blocks))
	height := 0
	for i, blk := range blocks {
		split[i] = strings.Split(blk, "\n")
		for _, l := range split[i] {
			if w := Width(l); w > widths[i] {
				widths[i] = w
			}
		}
		if len(split[i]) > height {
			height = len(split[i])
		}
	}
	var b strings.Builder
	for row := 0; row < height; row++ {
		var line strings.Builder
		for i := range blocks {
			cell := ""
			if row < len(split[i]) {
				cell = split[i][row]
			}
			if i < len(blocks)-1 {
				line.WriteString(PadRight(cell, widths[i]))
				line.WriteString(strings.Repeat(" ", gap))
			} else {
				line.WriteString(cell)
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		if row < height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
