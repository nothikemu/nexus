package ui

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// Tone is the semantic intent of a message.
type Tone int

// Message tones, each with its own glyph and colour.
const (
	ToneNexus   Tone = iota // ✦ nexus speaking
	ToneSuccess             // ◈ done / insight
	ToneWarning             // ▲ needs attention
	ToneError               // ✕ failed
	ToneInfo                // ◇ neutral
	ToneHint                // → next step
)

// Theme binds the palette to a renderer and exposes semantic styles.
type Theme struct {
	Mode    Mode
	Palette Palette
	Glyphs  Glyphs
	R       *lipgloss.Renderer

	Primary   lipgloss.Style
	Secondary lipgloss.Style
	Spark     lipgloss.Style
	Strong    lipgloss.Style
	Text      lipgloss.Style
	Muted     lipgloss.Style
	Faint     lipgloss.Style
	Success   lipgloss.Style
	Warning   lipgloss.Style
	Error     lipgloss.Style
	Info      lipgloss.Style

	Heading lipgloss.Style // small uppercase section headings
	Title   lipgloss.Style // headline of a Say block
	Code    lipgloss.Style // commands and identifiers inside prose
	Key     lipgloss.Style // labels in key/value lists
	Value   lipgloss.Style // values in key/value lists
	Number  lipgloss.Style // numeric values in tables
	Null    lipgloss.Style // SQL NULL
}

// NewTheme builds the theme for a mode. Output is the writer whose terminal
// capabilities determine colour depth (usually stdout).
func NewTheme(m Mode, output io.Writer) *Theme {
	r := lipgloss.NewRenderer(output)
	if !m.Color {
		r.SetColorProfile(termenv.Ascii)
	} else if r.ColorProfile() == termenv.Ascii {
		// Colour was forced (FORCE_COLOR) on a non-TTY: honour COLORTERM,
		// otherwise assume 256 colours.
		switch os.Getenv("COLORTERM") {
		case "truecolor", "24bit":
			r.SetColorProfile(termenv.TrueColor)
		default:
			r.SetColorProfile(termenv.ANSI256)
		}
	}
	r.SetHasDarkBackground(!m.Light)

	p := DarkPalette
	if m.Light {
		p = LightPalette
	}
	t := &Theme{Mode: m, Palette: p, R: r, Glyphs: glyphsFor(m)}
	c := func(hex string) lipgloss.Style { return r.NewStyle().Foreground(lipgloss.Color(hex)) }

	t.Primary = c(p.Primary)
	t.Secondary = c(p.Secondary)
	t.Spark = c(p.Spark)
	t.Strong = c(p.Strong).Bold(true)
	t.Text = c(p.Text)
	t.Muted = c(p.Muted)
	t.Faint = c(p.Faint)
	t.Success = c(p.Success)
	t.Warning = c(p.Warning)
	t.Error = c(p.Error)
	t.Info = c(p.Info)

	t.Heading = c(p.Muted).Bold(true)
	t.Title = c(p.Strong).Bold(true)
	t.Code = c(p.Secondary)
	t.Key = c(p.Muted)
	t.Value = c(p.Text)
	t.Number = c(p.Secondary)
	t.Null = c(p.Faint).Italic(true)
	return t
}

// Style returns a foreground style for an arbitrary hex colour.
func (t *Theme) Style(hex string) lipgloss.Style {
	return t.R.NewStyle().Foreground(lipgloss.Color(hex))
}

// Tone returns the style for a message tone.
func (t *Theme) Tone(tone Tone) lipgloss.Style {
	switch tone {
	case ToneSuccess:
		return t.Success
	case ToneWarning:
		return t.Warning
	case ToneError:
		return t.Error
	case ToneInfo:
		return t.Info
	case ToneHint:
		return t.Muted
	default:
		return t.Primary
	}
}

// ToneGlyph returns the coloured glyph that leads a message of this tone.
func (t *Theme) ToneGlyph(tone Tone) string {
	g := t.Glyphs
	var s string
	switch tone {
	case ToneSuccess:
		s = g.Insight
	case ToneWarning:
		s = g.Warning
	case ToneError:
		s = g.Error
	case ToneInfo:
		s = g.Info
	case ToneHint:
		s = g.Arrow
	default:
		s = g.Nexus
	}
	return t.Tone(tone).Bold(true).Render(s)
}

// Domain returns the accent style for a platform domain.
func (t *Theme) Domain(d Domain) lipgloss.Style {
	return t.Style(t.Palette.domain(d))
}

// Gradient colours each cell of s along a gradient between two hex colours.
// Without colour it returns s unchanged.
func (t *Theme) Gradient(s, from, to string) string {
	return t.gradient(s, from, to, false)
}

func (t *Theme) gradient(s, from, to string, bold bool) string {
	if !t.Mode.Color {
		return s
	}
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return s
	}
	a, errA := colorful.Hex(from)
	b, errB := colorful.Hex(to)
	if errA != nil || errB != nil {
		return s
	}
	var sb strings.Builder
	for i, r := range runes {
		if r == ' ' {
			sb.WriteRune(r)
			continue
		}
		f := 0.0
		if n > 1 {
			f = float64(i) / float64(n-1)
		}
		col := a.BlendLuv(b, f).Clamped().Hex()
		sb.WriteString(t.R.NewStyle().Foreground(lipgloss.Color(col)).Bold(bold).Render(string(r)))
	}
	return sb.String()
}

// Blend returns the hex colour at fraction f between two colours.
func Blend(from, to string, f float64) string {
	a, errA := colorful.Hex(from)
	b, errB := colorful.Hex(to)
	if errA != nil || errB != nil {
		return from
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return a.BlendLuv(b, f).Clamped().Hex()
}

// Wordmark renders the spaced N E X U S logotype on the core gradient.
func (t *Theme) Wordmark() string {
	word := "N E X U S"
	if !t.Mode.Color {
		return word
	}
	return t.gradient(word, t.Palette.CoreFrom, t.Palette.CoreTo, true)
}

// Cmd styles a shell command or identifier mentioned in prose.
func (t *Theme) Cmd(s string) string {
	if t.Mode.Plain {
		return "`" + s + "`"
	}
	return t.Code.Render(s)
}
