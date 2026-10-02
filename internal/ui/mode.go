// Package ui is the Nexus visual system: output modes, the colour palette,
// typography, components and the voice Nexus speaks in.
//
// Nothing in this package influences behaviour. Every component renders
// sensibly in four modes — rich, no-colour, plain and JSON — so the same
// command works in a 24-bit terminal, over SSH, in CI and for screen readers.
package ui

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
)

// Mode captures how output should be rendered for this invocation.
type Mode struct {
	Color       bool // emit ANSI colour
	Unicode     bool // use box drawing and unicode glyphs
	Animate     bool // spinners and mascot animation
	JSON        bool // machine-readable output only
	Plain       bool // no decoration at all: ASCII, no colour, no mascot
	Interactive bool // a human is at a terminal (stdin and stdout are TTYs, not CI)
	StderrTTY   bool // stderr is a terminal (where spinners live)
	Width       int  // terminal width in cells
	Light       bool // light terminal background
}

// Flags are the user-facing switches that influence Mode.
type Flags struct {
	JSON        bool
	Plain       bool
	NoColor     bool
	NoAnimation bool
}

// DetectMode resolves the output mode from flags, environment and terminal.
func DetectMode(f Flags) Mode {
	stdoutTTY := isTTY(os.Stdout)
	stdinTTY := isTTY(os.Stdin)
	stderrTTY := isTTY(os.Stderr)
	ci := isCI()

	m := Mode{
		Color:       stdoutTTY,
		Unicode:     true,
		Animate:     stdoutTTY && stderrTTY && !ci,
		Interactive: stdinTTY && stdoutTTY && !ci,
		StderrTTY:   stderrTTY,
		Width:       terminalWidth(),
		Light:       lightBackground(),
	}

	// Explicit colour forcing (https://force-color.org, CLICOLOR_FORCE).
	if os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") == "1" {
		m.Color = true
	}
	// https://no-color.org: any non-empty value disables colour.
	if os.Getenv("NO_COLOR") != "" || f.NoColor || os.Getenv("TERM") == "dumb" {
		m.Color = false
	}
	if f.NoAnimation || os.Getenv("NEXUS_NO_ANIMATION") != "" {
		m.Animate = false
	}
	if f.Plain || os.Getenv("NEXUS_PLAIN") != "" {
		m.Plain = true
		m.Color = false
		m.Unicode = false
		m.Animate = false
	}
	if f.JSON {
		m.JSON = true
		m.Color = false
		m.Animate = false
		m.Interactive = false
	}
	return m
}

// PlainMode is a fully undecorated mode, used by tests and fallbacks.
func PlainMode() Mode {
	return Mode{Plain: true, Width: 100}
}

// RichMode is a deterministic colour+unicode mode without animation,
// useful for previews and tests.
func RichMode(width int) Mode {
	return Mode{Color: true, Unicode: true, Width: width}
}

// ContentWidth is the usable width for blocks, leaving a small margin and
// clamping to a comfortable reading measure.
func (m Mode) ContentWidth() int {
	w := m.Width
	if w <= 0 {
		w = 80
	}
	if w > 110 {
		w = 110
	}
	if w < 40 {
		w = 40
	}
	return w - 2
}

func isTTY(f *os.File) bool {
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func isCI() bool {
	for _, k := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "JENKINS_URL", "TF_BUILD"} {
		if v := os.Getenv(k); v != "" && v != "0" && !strings.EqualFold(v, "false") {
			return true
		}
	}
	return false
}

func terminalWidth() int {
	if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 0 {
		return v
	}
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if w, _, err := term.GetSize(f.Fd()); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

// lightBackground decides the theme without querying the terminal (which can
// stall over SSH and inside tmux): NEXUS_THEME wins, then COLORFGBG.
func lightBackground() bool {
	switch strings.ToLower(os.Getenv("NEXUS_THEME")) {
	case "light":
		return true
	case "dark":
		return false
	}
	// COLORFGBG is "fg;bg" (sometimes "fg;default;bg"); bg 7 or 15 is light.
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		bg := parts[len(parts)-1]
		return bg == "7" || bg == "15"
	}
	return false
}
