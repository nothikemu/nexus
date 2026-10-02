package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nothikemu/nexus/internal/render"
	"runtime"
	"strings"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/localdb"
	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/project"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/sqltext"
	"github.com/nothikemu/nexus/internal/ui"
)

// problem translates any error into a user-facing Problem: what happened,
// why, and what to do next.
func (a *App) problem(err error) *ui.Problem {
	t := a.T()
	var (
		pr        *ui.Problem
		cfgErr    *config.Error
		missing   *config.MissingVarError
		portErr   *localdb.PortInUseError
		drift     *migrate.DriftError
		order     *migrate.OrderError
		empty     *migrate.EmptyError
		migErr    *migrate.Error
		parseErr  *migrate.ParseError
		irrev     *migrate.IrreversibleError
		confirmed *safety.ConfirmationRequiredError
	)
	switch {
	case errors.As(err, &pr):
		return pr
	case errors.Is(err, safety.ErrDeclined):
		return &ui.Problem{Title: "cancelled.", Detail: "nothing changed.", Exit: ui.ExitAborted, Code: "declined"}
	case errors.As(err, &confirmed):
		hint := "re-run with " + t.Cmd("--yes")
		if confirmed.Phrase != "" {
			hint = "re-run with " + t.Cmd(`--confirm "`+confirmed.Phrase+`"`)
		}
		return &ui.Problem{Title: "this needs confirmation.", Detail: "the terminal isn't interactive, so nexus can't ask.", Hint: hint, Exit: ui.ExitAborted, Code: "confirmation_required"}
	case errors.Is(err, context.Canceled):
		return &ui.Problem{Title: "interrupted.", Exit: ui.ExitInterrupted, Code: "interrupted"}
	case errors.Is(err, project.ErrNotFound):
		return &ui.Problem{Title: "not inside a nexus project.", Detail: "there's no " + config.FileName + " here or in any parent directory.",
			Hint: "create one with " + t.Cmd("nexus init my-app") + ", or explore any database with " + t.Cmd("--db-url"), Code: "no_project", Exit: ui.ExitUsage}
	case errors.Is(err, project.ErrExists):
		return &ui.Problem{Title: "there's already a nexus project here.", Hint: "start it with " + t.Cmd("nexus up"), Code: "project_exists", Exit: ui.ExitUsage}
	case errors.As(err, &cfgErr):
		return &ui.Problem{Title: config.FileName + " needs attention.", Detail: strings.Join(cfgErr.Problems, "\n"), Code: "invalid_config", Exit: ui.ExitUsage}
	case errors.As(err, &missing):
		return &ui.Problem{Title: "environment variable " + strings.Join(missing.Vars, ", ") + " isn't set.", Detail: missing.Field + " needs it.",
			Hint: "export it in your shell or CI secrets. Nexus never falls back to another environment's values.", Code: "missing_env_var", Exit: ui.ExitUsage}
	case errors.Is(err, localdb.ErrRoot):
		return &ui.Problem{Title: "PostgreSQL won't run as root.", Detail: "it refuses to, for safety, and nexus agrees.",
			Hint: "run nexus as a regular user, or set " + t.Cmd("database.runtime: docker") + " in " + config.FileName, Code: "root"}
	case errors.Is(err, localdb.ErrNoBinaries):
		return &ui.Problem{Title: "PostgreSQL isn't installed.", Detail: "nexus runs your local database with the PostgreSQL binaries on this machine, or with Docker.",
			Hint: installHint(t), Code: "no_postgres"}
	case errors.Is(err, localdb.ErrNoDocker):
		return &ui.Problem{Title: "docker isn't available.", Detail: err.Error(), Hint: "start Docker, or set " + t.Cmd("database.runtime: native") + " and install PostgreSQL", Code: "no_docker"}
	case errors.As(err, &portErr):
		return &ui.Problem{Title: fmt.Sprintf("port %d is taken.", portErr.Port), Detail: "another process is listening on it.",
			Hint: "change database.port in " + config.FileName, Code: "port_in_use"}
	case errors.Is(err, localdb.ErrNotRunning):
		return &ui.Problem{Title: ui.SayFirst(ui.MomentAsleep), Detail: "the local database isn't running.", Hint: "wake it with " + t.Cmd("nexus up"), Code: "not_running", Exit: ui.ExitUnreachable}
	case errors.Is(err, localdb.ErrExternal):
		return &ui.Problem{Title: "this project uses an external database.", Detail: "nexus connects to it but never starts, stops or deletes it.", Code: "external"}
	case errors.As(err, &drift):
		var lines []string
		for _, e := range drift.Modified {
			lines = append(lines, t.Warning.Render("~ ")+e.Version+"_"+e.Name+t.Muted.Render("  edited after it was applied"))
		}
		for _, e := range drift.Missing {
			lines = append(lines, t.Error.Render("− ")+e.Version+"_"+e.Name+t.Muted.Render("  applied, but the file is gone"))
		}
		return &ui.Problem{Title: "applied migrations changed on disk.", Detail: strings.Join(lines, "\n"),
			Hint: "restore the files, or accept the current state with " + t.Cmd("nexus migration repair"), Code: "migration_drift"}
	case errors.As(err, &order):
		var lines []string
		for _, e := range order.Entries {
			lines = append(lines, e.Version+"_"+e.Name)
		}
		return &ui.Problem{Title: "pending migrations are older than the latest applied one.", Detail: strings.Join(lines, "\n") + "\n\nthis usually happens after merging a branch.",
			Hint: "apply them anyway with " + t.Cmd("nexus migration apply --allow-out-of-order"), Code: "out_of_order"}
	case errors.As(err, &empty):
		return &ui.Problem{Title: "migration " + empty.Migration.ID() + " is empty.", Detail: "write some SQL under -- nexus:up first.", Hint: a.relPath(empty.Migration.Path), Code: "empty_migration"}
	case errors.As(err, &migErr):
		return a.migrationProblem(migErr)
	case errors.As(err, &parseErr):
		return &ui.Problem{Title: "can't read a migration file.", Detail: parseErr.Error(), Code: "migration_parse"}
	case errors.As(err, &irrev):
		return &ui.Problem{Title: "can't roll back.", Detail: irrev.Error(), Hint: "add a -- nexus:down section, or write a new migration that undoes it", Code: "irreversible"}
	case errors.Is(err, migrate.ErrLocked):
		return &ui.Problem{Title: "another migration is running.", Detail: "nexus waited, but the migration lock is still held.", Hint: "try again when it's done", Code: "migration_locked"}
	}
	if up, ok := usageError(err); ok {
		up.Hint = "see nexus --help"
		return up
	}
	if pe, ok := pg.AsPgError(err); ok {
		return a.sqlProblem(err, "", 0, pe.Position)
	}
	if pg.IsUnreachable(err) {
		return &ui.Problem{Title: "couldn't reach the database.", Detail: err.Error(), Exit: ui.ExitUnreachable, Code: "database_unreachable"}
	}
	return &ui.Problem{Title: ui.SayFirst(ui.MomentBroke), Detail: err.Error(), Err: err}
}

// sqlProblem renders a PostgreSQL error with an optional code frame.
func (a *App) sqlProblem(err error, source string, lineOffset int, position int32) *ui.Problem {
	return render.SQLProblem(a.T(), err, source, lineOffset, position)
}

func (a *App) migrationProblem(me *migrate.Error) *ui.Problem {
	pe, ok := pg.AsPgError(me.Err)
	if !ok {
		return &ui.Problem{Title: "migration " + me.Migration.ID() + " failed.", Detail: me.Err.Error(), Err: me}
	}
	pr := a.sqlProblem(me.Err, me.SQL, me.LineOffset, pe.Position)
	pr.Title = "migration " + me.Migration.ID() + " failed."
	msg := pe.Message
	if pe.Detail != "" {
		msg += "\n" + pe.Detail
	}
	where := a.relPath(me.Migration.Path)
	if pe.Position > 0 {
		off := sqltext.CharToByte(me.SQL, int(pe.Position))
		line, col := sqltext.LineCol(me.SQL, off)
		where = fmt.Sprintf("%s:%d:%d", where, line+me.LineOffset, col)
	}
	pr.Detail = msg + "\n" + a.T().Muted.Render(where+" ("+me.Section+" section, rolled back)")
	if pe.Hint == "" {
		pr.Hint = "fix the file and run " + a.T().Cmd("nexus migration apply") + " again"
	}
	return pr
}

func (a *App) relPath(path string) string {
	if p, err := a.Project(); err == nil {
		return p.Rel(path)
	}
	return path
}

func installHint(t *ui.Theme) string {
	switch runtime.GOOS {
	case "darwin":
		return "install it with " + t.Cmd("brew install postgresql@16") + ", or use Docker (database.runtime: docker)"
	case "windows":
		return "install it from postgresql.org/download/windows, or use Docker (database.runtime: docker)"
	default:
		return "install it with " + t.Cmd("sudo apt install postgresql") + " (or your distro's package), or use Docker (database.runtime: docker)"
	}
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte(`{"error":{"title":"failed to encode error"}}`)
	}
	return append(b, '\n')
}
