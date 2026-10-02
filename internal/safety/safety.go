// Package safety classifies operations and enforces confirmation.
//
// Nothing destructive happens silently. Destructive operations show what
// they will do and ask first; against a protected environment they demand
// a typed phrase, and in CI they require that phrase via --confirm.
package safety

import (
	"errors"
	"strings"

	"github.com/nothikemu/nexus/internal/ui"
)

// Level classifies what an operation can do.
type Level int

// Safety levels.
const (
	ReadOnly    Level = iota // reads only
	SafeWrite                // writes that don't destroy data (apply a migration, seed)
	Destructive              // may permanently delete data
)

func (l Level) String() string {
	switch l {
	case SafeWrite:
		return "SAFE WRITE"
	case Destructive:
		return "DESTRUCTIVE"
	default:
		return "READ ONLY"
	}
}

// Target is where an operation runs.
type Target struct {
	Env       string // local, staging, production, or "url" for --db-url
	Protected bool
	Database  string // display name, e.g. postgres@127.0.0.1:54320/my_app
}

// Operation describes something about to happen.
type Operation struct {
	Level   Level
	Action  string   // short description: "roll back 2 migrations"
	Details []string // the specifics, one per line
	// AlwaysAsk confirms a safe write even on unprotected environments.
	AlwaysAsk bool
}

// Errors.
var (
	// ErrDeclined means the user said no. Nothing changed.
	ErrDeclined = errors.New("cancelled — nothing changed")
)

// ConfirmationRequiredError means confirmation is needed but can't be asked for.
type ConfirmationRequiredError struct {
	Phrase string // non-empty when a typed phrase is required
}

func (e *ConfirmationRequiredError) Error() string {
	if e.Phrase != "" {
		return `this needs confirmation: pass --confirm "` + e.Phrase + `"`
	}
	return "this needs confirmation: pass --yes"
}

// Phrase is the text a user types to confirm a destructive operation on a
// protected environment: DELETE PRODUCTION DATA.
func Phrase(env string) string {
	return "DELETE " + strings.ToUpper(env) + " DATA"
}

// Guard asks for confirmation according to the operation and target.
type Guard struct {
	P       *ui.Printer
	Yes     bool   // --yes: skip ordinary confirmations
	Confirm string // --confirm: the typed phrase, for non-interactive use
}

// Allow returns nil when the operation may proceed.
func (g *Guard) Allow(op Operation, t Target) error {
	switch op.Level {
	case ReadOnly:
		return nil
	case SafeWrite:
		if (!t.Protected && !op.AlwaysAsk) || g.Yes {
			return nil
		}
		g.warn(op, t)
		return g.ask(op.Action + " on " + t.Env + "?")
	}

	// Destructive.
	g.warn(op, t)
	if t.Protected {
		phrase := Phrase(t.Env)
		if g.Confirm != "" {
			if g.Confirm == phrase {
				return nil
			}
			return &ConfirmationRequiredError{Phrase: phrase}
		}
		ok, err := g.P.ConfirmPhrase(phrase)
		if errors.Is(err, ui.ErrNotInteractive) {
			return &ConfirmationRequiredError{Phrase: phrase}
		}
		if err != nil {
			return err
		}
		if !ok {
			return ErrDeclined
		}
		return nil
	}
	if g.Yes {
		return nil
	}
	return g.ask("continue?")
}

func (g *Guard) ask(question string) error {
	ok, err := g.P.Confirm(question, false)
	if errors.Is(err, ui.ErrNotInteractive) {
		return &ConfirmationRequiredError{}
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrDeclined
	}
	return nil
}

// warn shows what is about to happen, on stderr next to the prompt.
func (g *Guard) warn(op Operation, t Target) {
	if g.P.Quiet() {
		return
	}
	th := g.P.T
	head := t.Env + " database"
	if t.Protected {
		head = strings.ToLower(t.Env) + " database · protected"
	}
	var body []string
	if op.Level == Destructive {
		body = append(body, th.Text.Render("This operation may permanently delete data."))
	}
	if op.Action != "" || len(op.Details) > 0 {
		lines := []string{th.Strong.Render(op.Action)}
		for _, d := range op.Details {
			lines = append(lines, th.Muted.Render(th.Glyphs.Bullet+" ")+d)
		}
		body = append(body, strings.Join(lines, "\n"))
	}
	if t.Database != "" {
		body = append(body, th.Muted.Render(t.Database)+"  "+th.Faint.Render(op.Level.String()))
	}
	g.P.ErrBlock(th.SayString(ui.ToneWarning, head, body...))
}
