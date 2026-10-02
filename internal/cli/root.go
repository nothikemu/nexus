package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/logging"
	"github.com/nothikemu/nexus/internal/ui"
)

// Command groups shown in help.
const (
	groupStart    = "start"
	groupDatabase = "database"
	groupMore     = "more"
)

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &App{Flags: &Flags{}}
	root := newRoot(app)
	err := root.ExecuteContext(ctx)
	app.Close()
	if app.P != nil {
		ui.RestoreCursor(app.P)
	}
	if err == nil {
		return ui.ExitOK
	}
	if app.P == nil {
		app.P = ui.NewPrinter(app.mode())
	}
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return ui.ExitInterrupted
	}
	return app.report(err)
}

func (a *App) mode() ui.Mode {
	return ui.DetectMode(ui.Flags{JSON: a.Flags.JSON, Plain: a.Flags.Plain, NoColor: a.Flags.NoColor, NoAnimation: a.Flags.NoAnimation})
}

func newRoot(app *App) *cobra.Command {
	cobra.EnableCommandSorting = false
	root := &cobra.Command{
		Use:   "nexus",
		Short: "your database, right in the terminal.",
		Long: "Nexus is a PostgreSQL-native backend platform that lives in your terminal.\n" +
			"Run it without a command to open the live dashboard.",
		Example:           "  nexus init my-app\n  cd my-app && nexus dev\n  nexus sql \"select * from users limit 5\"",
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			app.P = ui.NewPrinter(app.mode())
			logging.Setup(os.Stderr, app.Flags.Verbose)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return unknownCommand(cmd, args[0])
			}
			m := app.P.Mode()
			if m.Interactive && !m.Plain && !m.JSON {
				return runDashboard(cmd.Context(), app)
			}
			return runStatus(cmd.Context(), app)
		},
		Args: cobra.ArbitraryArgs,
	}

	f := root.PersistentFlags()
	f.BoolVar(&app.Flags.JSON, "json", false, "machine-readable JSON output")
	f.BoolVar(&app.Flags.Plain, "plain", false, "no colour, no animation, ASCII only (CI and screen readers)")
	f.BoolVar(&app.Flags.NoColor, "no-color", false, "disable colour (also NO_COLOR)")
	f.BoolVar(&app.Flags.NoAnimation, "no-animation", false, "disable spinners and animation")
	f.StringVarP(&app.Flags.Env, "env", "e", "", "target environment (default local, or NEXUS_ENV)")
	f.StringVar(&app.Flags.DBURL, "db-url", "", "connect to this database instead (or NEXUS_DATABASE_URL)")
	f.StringVarP(&app.Flags.ProjectDir, "project", "C", "", "run as if started in this directory")
	f.BoolVarP(&app.Flags.Yes, "yes", "y", false, "skip confirmations (never enough for protected environments)")
	f.StringVar(&app.Flags.Confirm, "confirm", "", "typed confirmation phrase for protected environments in CI")
	f.BoolVarP(&app.Flags.Verbose, "verbose", "v", false, "log diagnostics to stderr")

	root.AddGroup(
		&cobra.Group{ID: groupStart, Title: "Start"},
		&cobra.Group{ID: groupDatabase, Title: "Database"},
		&cobra.Group{ID: groupMore, Title: "More"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add(groupStart, newInitCmd(app), newDevCmd(app), newStatusCmd(app), newDoctorCmd(app))
	add(groupDatabase, newSQLCmd(app), newDBCmd(app), newTableCmd(app), newMigrationCmd(app), newQueryCmd(app))
	add(groupMore, newMascotCmd(app), newVersionCmd(app))
	root.SetHelpCommandGroupID(groupMore)
	root.SetCompletionCommandGroupID(groupMore)

	root.SuggestionsMinimumDistance = 2
	installHelp(root, app)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &ui.Problem{Title: err.Error() + ".", Hint: "see " + cmd.CommandPath() + " --help", Code: "usage", Exit: ui.ExitUsage}
	})
	return root
}

// report renders an error and returns the exit code.
func (a *App) report(err error) int {
	pr := a.problem(err)
	if pr == nil {
		return ui.ExitOK
	}
	if a.P.Mode().JSON {
		enc := pr.JSON()
		a.P.Err.Write(mustJSON(enc))
		return pr.ExitCode()
	}
	if pr.Exit == ui.ExitAborted {
		a.P.Say(ui.ToneInfo, pr.Title, pr.Detail)
		return pr.ExitCode()
	}
	fmt.Fprintln(a.P.Err)
	fmt.Fprintln(a.P.Err, a.T().ProblemString(pr))
	fmt.Fprintln(a.P.Err)
	return pr.ExitCode()
}

func unknownCommand(cmd *cobra.Command, name string) error {
	pr := &ui.Problem{Title: fmt.Sprintf("nexus doesn't know %q.", name), Code: "unknown_command", Exit: ui.ExitUsage}
	if s := cmd.SuggestionsFor(name); len(s) > 0 {
		pr.Hint = "did you mean nexus " + s[0] + "?"
	} else {
		pr.Hint = "see nexus --help"
	}
	return pr
}

func itoa(i int) string { return strconv.Itoa(i) }

// usageError maps cobra's argument validation messages to a usage problem.
func usageError(err error) (*ui.Problem, bool) {
	msg := err.Error()
	for _, prefix := range []string{"accepts ", "requires ", "unknown command ", "invalid argument "} {
		if strings.HasPrefix(msg, prefix) {
			return &ui.Problem{Title: msg + ".", Code: "usage", Exit: ui.ExitUsage}, true
		}
	}
	return nil, false
}
