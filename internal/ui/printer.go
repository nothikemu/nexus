package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Printer writes Nexus output with consistent rhythm: blocks are separated by
// exactly one blank line and body text is indented two cells under its
// headline glyph. Results go to Out; progress and prompts go to Err so that
// stdout stays clean for pipes.
type Printer struct {
	T   *Theme
	Out io.Writer
	Err io.Writer
	In  io.Reader

	mu        sync.Mutex
	lastBlank bool // last line written to Out was blank
	started   bool // anything written to Out yet
	reader    *bufio.Reader
}

// NewPrinter creates a printer for the given mode on stdio.
func NewPrinter(m Mode) *Printer {
	return &Printer{T: NewTheme(m, os.Stdout), Out: os.Stdout, Err: os.Stderr, In: os.Stdin}
}

// NewPrinterTo creates a printer writing to the given streams.
func NewPrinterTo(m Mode, out, err io.Writer) *Printer {
	return &Printer{T: NewTheme(m, out), Out: out, Err: err, In: os.Stdin}
}

// NewTestPrinter renders into buffers with the given mode and no input.
func NewTestPrinter(m Mode, out, err io.Writer) *Printer {
	p := NewPrinterTo(m, out, err)
	p.In = strings.NewReader("")
	return p
}

// Mode is shorthand for the theme's mode.
func (p *Printer) Mode() Mode { return p.T.Mode }

// Quiet reports whether human output is suppressed (JSON mode).
func (p *Printer) Quiet() bool { return p.T.Mode.JSON }

// Block writes a self-contained block of text, separated from the previous
// block by one blank line.
func (p *Printer) Block(s string) {
	if p.Quiet() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s = strings.TrimRight(s, "\n")
	if p.started && !p.lastBlank {
		fmt.Fprintln(p.Out)
	}
	fmt.Fprintln(p.Out, s)
	fmt.Fprintln(p.Out)
	p.started = true
	p.lastBlank = true
}

// Line writes a single line without block spacing.
func (p *Printer) Line(s string) {
	if p.Quiet() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintln(p.Out, s)
	p.started = true
	p.lastBlank = strings.TrimSpace(s) == ""
}

// Raw writes s exactly, for streamed content such as logs.
func (p *Printer) Raw(s string) {
	if p.Quiet() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprint(p.Out, s)
	p.started = true
	p.lastBlank = strings.HasSuffix(s, "\n\n")
}

// Say prints a voice message: a glyph-led headline followed by indented
// paragraphs.
//
//	✦ nexus is awake.
//
//	  Connected to PostgreSQL
//	  14 tables · 2.8 MB · 0 problems
func (p *Printer) Say(tone Tone, headline string, paragraphs ...string) {
	p.Block(p.T.SayString(tone, headline, paragraphs...))
}

// SayString renders a voice message without printing it.
func (t *Theme) SayString(tone Tone, headline string, paragraphs ...string) string {
	var b strings.Builder
	b.WriteString(t.ToneGlyph(tone))
	b.WriteString(" ")
	b.WriteString(t.Tone(tone).Bold(true).Render(headline))
	for _, para := range paragraphs {
		if para == "" {
			continue
		}
		b.WriteString("\n\n")
		b.WriteString(Indent(para, 2))
	}
	return b.String()
}

// Hint prints "→ text" in muted style, attached to the previous block.
func (p *Printer) Hint(text string) {
	p.Line(p.T.HintString(text))
}

// HintString renders a hint line.
func (t *Theme) HintString(text string) string {
	return "  " + t.Muted.Render(t.Glyphs.Arrow+" "+text)
}

// JSON writes v as indented JSON to stdout. It is the only output in --json mode.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Errorf writes a line to stderr (never suppressed).
func (p *Printer) Errorf(format string, args ...any) {
	fmt.Fprintf(p.Err, format, args...)
}

// readLine reads a line from the printer's input.
func (p *Printer) readLine() (string, error) {
	if p.reader == nil {
		p.reader = bufio.NewReader(p.In)
	}
	s, err := p.reader.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// ErrBlock writes a block to stderr (warnings next to prompts). It is shown
// even in --json mode, because prompts and warnings are for humans.
func (p *Printer) ErrBlock(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintln(p.Err)
	fmt.Fprintln(p.Err, strings.TrimRight(s, "\n"))
	fmt.Fprintln(p.Err)
}
