package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNotInteractive is returned when a prompt is needed but no human is at
// the terminal (CI, pipes, --json).
var ErrNotInteractive = errors.New("confirmation required, but the terminal is not interactive")

// Confirm asks a yes/no question on stderr.
func (p *Printer) Confirm(question string, defaultYes bool) (bool, error) {
	if !p.T.Mode.Interactive {
		return false, ErrNotInteractive
	}
	th := p.T
	choices := "y/N"
	if defaultYes {
		choices = "Y/n"
	}
	fmt.Fprintf(p.Err, "  %s %s %s ", th.Primary.Bold(true).Render(th.Glyphs.Prompt), th.Text.Render(question), th.Muted.Render("("+choices+")"))
	ans, err := p.readLine()
	if err != nil {
		fmt.Fprintln(p.Err)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	case "":
		return defaultYes, nil
	default:
		return false, nil
	}
}

// ConfirmPhrase asks the user to type an exact phrase. It is used for
// operations against protected environments:
//
//	Type DELETE PRODUCTION DATA to continue:
func (p *Printer) ConfirmPhrase(phrase string) (bool, error) {
	if !p.T.Mode.Interactive {
		return false, ErrNotInteractive
	}
	th := p.T
	fmt.Fprintf(p.Err, "  %s\n\n    %s\n\n  %s ", th.Text.Render("Type the following to continue:"), th.Warning.Bold(true).Render(phrase), th.Primary.Bold(true).Render(th.Glyphs.Prompt))
	ans, err := p.readLine()
	if err != nil {
		fmt.Fprintln(p.Err)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(ans) == phrase, nil
}

// Ask prompts for a line of text with a default.
func (p *Printer) Ask(question, def string) (string, error) {
	if !p.T.Mode.Interactive {
		return def, nil
	}
	th := p.T
	suffix := ""
	if def != "" {
		suffix = " " + th.Muted.Render("("+def+")")
	}
	fmt.Fprintf(p.Err, "  %s %s%s ", th.Primary.Bold(true).Render(th.Glyphs.Prompt), th.Text.Render(question), suffix)
	ans, err := p.readLine()
	if err != nil {
		fmt.Fprintln(p.Err)
		if errors.Is(err, io.EOF) {
			return def, nil
		}
		return "", err
	}
	ans = strings.TrimSpace(ans)
	if ans == "" {
		return def, nil
	}
	return ans, nil
}
