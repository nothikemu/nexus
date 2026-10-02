package ui

import (
	"fmt"
	"strings"
)

// Exit codes used across the CLI.
const (
	ExitOK          = 0
	ExitFailure     = 1
	ExitUsage       = 2
	ExitAborted     = 3
	ExitUnreachable = 4
	ExitInterrupted = 130
)

// Problem is a user-facing error: what happened, why, and what to do next.
// Every error that reaches the top of the CLI is rendered as a Problem.
type Problem struct {
	Title  string // short, lowercase, calm: "couldn't reach the database."
	Detail string // the underlying error text
	Hint   string // the next step: "start it with nexus dev"
	Frame  string // optional pre-rendered code frame
	Code   string // stable machine-readable code (or SQLSTATE)
	Exit   int    // process exit code
	Err    error  // wrapped cause
}

func (p *Problem) Error() string {
	parts := []string{strings.TrimSuffix(p.Title, ".")}
	if p.Detail != "" {
		parts = append(parts, p.Detail)
	}
	return strings.Join(parts, ": ")
}

func (p *Problem) Unwrap() error { return p.Err }

// ExitCode returns the process exit code for the problem.
func (p *Problem) ExitCode() int {
	if p.Exit == 0 {
		return ExitFailure
	}
	return p.Exit
}

// Problemf builds a Problem with a formatted title.
func Problemf(format string, args ...any) *Problem {
	return &Problem{Title: fmt.Sprintf(format, args...)}
}

// WithHint returns p with a hint attached.
func (p *Problem) WithHint(format string, args ...any) *Problem {
	p.Hint = fmt.Sprintf(format, args...)
	return p
}

// WithDetail returns p with detail text attached.
func (p *Problem) WithDetail(format string, args ...any) *Problem {
	p.Detail = fmt.Sprintf(format, args...)
	return p
}

// ProblemString renders a problem for the terminal.
func (t *Theme) ProblemString(p *Problem) string {
	var paras []string
	if p.Detail != "" {
		paras = append(paras, t.Text.Render(Wrap(p.Detail, t.Mode.ContentWidth()-4)))
	}
	if p.Frame != "" {
		paras = append(paras, p.Frame)
	}
	if p.Hint != "" {
		paras = append(paras, t.Muted.Render(t.Glyphs.Arrow+" ")+t.Text.Render(p.Hint))
	}
	return t.SayString(ToneError, p.Title, paras...)
}

// ProblemJSON is the JSON shape of a problem in --json mode.
type ProblemJSON struct {
	Error struct {
		Title  string `json:"title"`
		Detail string `json:"detail,omitempty"`
		Hint   string `json:"hint,omitempty"`
		Code   string `json:"code,omitempty"`
	} `json:"error"`
}

// JSON converts a problem to its JSON shape.
func (p *Problem) JSON() ProblemJSON {
	var j ProblemJSON
	j.Error.Title = strings.TrimSuffix(p.Title, ".")
	j.Error.Detail = p.Detail
	j.Error.Hint = p.Hint
	j.Error.Code = p.Code
	return j
}

// CodeFrame renders source lines around a 1-based character position with a
// caret marking the column, like a compiler diagnostic:
//
//	4 │   id uuid primary key,
//	5 │   email txt not null
//	  │         ^
//
// lineOffset shifts the displayed line numbers (when source is an excerpt of
// a larger file). message is shown after the caret.
func (t *Theme) CodeFrame(source string, position int, lineOffset int, message string) string {
	if position <= 0 {
		return ""
	}
	runes := []rune(source)
	if position > len(runes)+1 {
		return ""
	}
	line, col := 1, 1
	for i := 0; i < position-1 && i < len(runes); i++ {
		if runes[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	lines := strings.Split(source, "\n")
	from := line - 2
	if from < 1 {
		from = 1
	}
	numW := len(fmt.Sprint(line + lineOffset))
	pipe := t.Faint.Render("│")
	var b strings.Builder
	for n := from; n <= line && n <= len(lines); n++ {
		text := strings.ReplaceAll(lines[n-1], "\t", "    ")
		num := fmt.Sprintf("%*d", numW, n+lineOffset)
		style := t.Muted
		if n == line {
			style = t.Text
		}
		b.WriteString(t.Faint.Render(num) + " " + pipe + " " + style.Render(text) + "\n")
	}
	// Account for tabs expanded before the caret.
	prefix := []rune(lines[line-1])
	caretCol := 0
	for i := 0; i < col-1 && i < len(prefix); i++ {
		if prefix[i] == '\t' {
			caretCol += 4
		} else {
			caretCol++
		}
	}
	caret := strings.Repeat(" ", caretCol) + t.Error.Bold(true).Render("^")
	if message != "" {
		caret += " " + t.Error.Render(message)
	}
	b.WriteString(strings.Repeat(" ", numW) + " " + pipe + " " + caret)
	return b.String()
}
