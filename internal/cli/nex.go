package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

// This file is Nex's personality: greetings, the welcome screen, pats, tips
// and the guide. None of it changes what any command does.

// greeting picks words for the time of day.
func greeting(now time.Time) string {
	h := now.Hour()
	switch {
	case h >= 5 && h < 12:
		if now.Weekday() == time.Monday {
			return "good morning. monday again — i've got coffee-flavoured queries ready."
		}
		return "good morning."
	case h >= 12 && h < 18:
		if now.Weekday() == time.Friday {
			return "good afternoon. happy friday."
		}
		return "good afternoon."
	case h >= 18 && h < 23:
		return "good evening."
	default:
		return "up late? me too. databases never really sleep."
	}
}

var tips = []string{
	"press w on the dashboard to wake a sleeping database.",
	"nexus migration diff shows what a migration changes — then undoes it.",
	"nexus query analyze tells you why a query is slow, and how to fix it.",
	"in nexus table <name> browse, press g on a foreign key to jump to the row it points at.",
	"nexus sql remembers your history. press ↑ to bring back a query.",
	"add --json to almost anything to get machine-readable output.",
	"export DATABASE_URL=$(nexus db url) wires your app to the local database.",
	"nexus doctor checks your whole setup in a couple of seconds.",
	"nexus db inspect finds missing indexes and other quiet problems.",
	"\\explain <query> in the SQL shell shows the plan without leaving it.",
	"nexus up and nexus down start and stop the local database.",
	"migrations run in transactions: if one fails, nothing half-applies.",
}

// tip picks a tip that changes daily.
func tip(now time.Time) string {
	h := fnv.New32a()
	h.Write([]byte(now.Format("2006-01-02")))
	return tips[int(h.Sum32())%len(tips)]
}

// nexState is the little bit Nex remembers about you, per user.
type nexState struct {
	Pats     int       `json:"pats"`
	FirstMet time.Time `json:"first_met"`
	LastPat  time.Time `json:"last_pat,omitempty"`
}

func nexStatePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "nexus", "nex.json")
}

func loadNex() *nexState {
	s := &nexState{FirstMet: time.Now()}
	if p := nexStatePath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, s)
		}
	}
	return s
}

func (s *nexState) save() {
	p := nexStatePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(p, b, 0o600)
}

// speak shows Nex in a mood saying something, animating when allowed.
func speak(ctx context.Context, app *App, state mascot.State, text string) {
	t := app.T()
	if mascot.Portrait(t, state, 0) == "" {
		app.P.Say(ui.ToneNexus, text)
		return
	}
	render := func(s mascot.State, n int) string {
		return ui.Indent(t.Speak(mascot.Portrait(t, s, n), text, 52), 2)
	}
	app.P.Line("")
	if mascot.CanAnimate(t) && mascot.Loop(state) > 1 {
		mascot.Play(ctx, app.P.Out, []mascot.Step{{State: state, Frames: mascot.Loop(state) * 2}}, mascot.FrameInterval, render)
	} else {
		app.P.Line(render(state, 0))
	}
	app.P.Line("")
}

func newHiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "hi",
		Aliases: []string{"hello", "hey"},
		Short:   "say hi to nex",
		Long:    "Nex says hello, tells you how your project is doing, and shares a tip.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			now := time.Now()
			lines := []string{greeting(now)}
			if app.HasProject() {
				r, err := collectStatus(ctx, app)
				if err == nil {
					lines = append(lines, projectMood(r))
				}
			} else {
				lines = append(lines, "i'm nex. i look after your database.")
			}
			lines = append(lines, "tip: "+tip(now))
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]any{"name": mascot.Name, "says": lines})
			}
			speak(ctx, app, mascot.Waving, strings.Join(lines, "\n\n"))
			return nil
		},
	}
}

// projectMood is Nex's one-line read on a status report.
func projectMood(r *statusReport) string {
	name := r.Project
	if name == "" {
		name = "your database"
	}
	switch {
	case !r.Online:
		return name + " is napping. say nexus up to wake it."
	case r.Migrations != nil && r.Migrations.Pending > 0:
		return fmt.Sprintf("%s is awake, with %s waiting. nexus migration apply when you're ready.", name, ui.Plural(int64(r.Migrations.Pending), "migration", "migrations"))
	case len(r.Problems) > 0:
		return fmt.Sprintf("%s is awake. i spotted %s — nexus db inspect has details.", name, ui.Plural(int64(len(r.Problems)), "thing", "things"))
	case r.Tables == 0:
		return name + " is awake and empty. a blank page. exciting."
	default:
		return fmt.Sprintf("%s is awake and healthy: %s, all up to date.", name, ui.Plural(int64(r.Tables), "table", "tables"))
	}
}

func newPetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "pet",
		Aliases: []string{"pat"},
		Short:   "give nex a pat",
		Long:    "Nex keeps count. It doesn't do anything else. Sometimes that's the point.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := loadNex()
			s.Pats++
			s.LastPat = time.Now()
			s.save()
			var says string
			switch {
			case s.Pats == 1:
				says = "oh! hello. that was nice."
			case s.Pats == 10:
				says = "ten pats. we're friends now. official."
			case s.Pats == 50:
				says = "fifty pats. i'm writing this one down in nexus_meta. (i'm not.)"
			case s.Pats == 100:
				says = "one hundred pats. i'd frame it if i had walls."
			case s.Pats%25 == 0:
				says = fmt.Sprintf("%d pats. you're very consistent. i respect that.", s.Pats)
			default:
				says = []string{"aw. thank you.", "hehe.", "that's the spot.", "*happy query noises*", "i'll make your next migration extra smooth."}[s.Pats%5]
			}
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]any{"pats": s.Pats, "says": says})
			}
			speak(cmd.Context(), app, mascot.Love, says+"\n\n"+app.T().Muted.Render(ui.Plural(int64(s.Pats), "pat", "pats")+" so far"))
			return nil
		},
	}
}

// runWelcome greets someone running nexus outside a project.
func runWelcome(ctx context.Context, app *App) error {
	t := app.T()
	if app.P.Mode().JSON {
		return app.P.JSON(map[string]any{"project": nil, "next": []string{"nexus init my-app", "nexus --db-url <url>", "nexus guide"}})
	}
	speak(ctx, app, mascot.Waving, greeting(time.Now())+"\n\ni'm nex. i look after your database. there's no project here yet — want to start one?")
	steps := [][2]string{
		{"nexus init my-app", "create a project (takes a second)"},
		{"nexus --db-url <url>", "or explore a database you already have"},
		{"nexus guide", "everything nexus can do, on one screen"},
	}
	var lines []string
	for _, s := range steps {
		lines = append(lines, t.Code.Render(ui.PadRight(s[0], 24))+t.Muted.Render(s[1]))
	}
	app.P.Block(ui.Indent(strings.Join(lines, "\n"), 2))
	return nil
}

func newGuideCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "guide",
		Aliases: []string{"cheatsheet", "tour"},
		Short:   "everything nexus can do, on one screen",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sections := []struct {
				title string
				rows  [][2]string
			}{
				{"start", [][2]string{
					{"nexus init my-app", "create a project"},
					{"nexus up", "start the local database (same as nexus dev)"},
					{"nexus down", "stop it — your data stays"},
					{"nexus", "open the live dashboard"},
				}},
				{"look around", [][2]string{
					{"nexus tables", "list your tables"},
					{"nexus table users", "columns, indexes and relationships"},
					{"nexus browse users", "explore rows: search, filter, sort, edit"},
					{"nexus sql", "the SQL shell"},
				}},
				{"change the schema", [][2]string{
					{"nexus migration create add_posts", "a new migration file"},
					{"nexus migration diff", "preview what it changes"},
					{"nexus migration apply", "apply it"},
					{"nexus migration rollback", "undo the last one"},
				}},
				{"make it fast", [][2]string{
					{`nexus query analyze "select …"`, "where the time goes, and fixes"},
					{"nexus db inspect", "missing indexes and other problems"},
				}},
				{"when something's off", [][2]string{
					{"nexus doctor", "check everything"},
					{"nexus status", "what's running"},
					{"nexus dev logs -f", "the database's own logs"},
				}},
			}
			if app.P.Mode().JSON {
				out := map[string][][2]string{}
				for _, s := range sections {
					out[s.title] = s.rows
				}
				return app.P.JSON(out)
			}
			t := app.T()
			if face := mascot.Face(t, mascot.Waving, 0); face != "" {
				app.P.Block(face + "  " + t.Text.Render("here's everything you need. it fits on one screen."))
			} else {
				app.P.Say(ui.ToneNexus, "here's everything you need.")
			}
			var blocks []string
			for _, s := range sections {
				var lines []string
				for _, r := range s.rows {
					lines = append(lines, t.Code.Render(ui.PadRight(r[0], 34))+t.Muted.Render(r[1]))
				}
				blocks = append(blocks, t.Section(s.title, strings.Join(lines, "\n")))
			}
			app.P.Block(ui.Indent(strings.Join(blocks, "\n\n"), 2))
			app.P.Hint("every command has --help · full docs: github.com/nothikemu/nexus/tree/main/docs")
			return nil
		},
	}
}

// Short, friendly entry points for the commands people use most.

func newDownCmd(app *App) *cobra.Command {
	stop := newDevStopCmd(app)
	return &cobra.Command{
		Use:   "down",
		Short: "stop the local database (same as nexus dev stop)",
		Args:  cobra.NoArgs,
		RunE:  stop.RunE,
	}
}

func newTablesCmd(app *App) *cobra.Command {
	c := newDBTablesCmd(app)
	c.Short = "list your tables (same as nexus db tables)"
	return c
}

func newBrowseCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "browse <table>",
		Short: "explore a table's rows interactively",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeTables(cmd.Context(), app), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			snap, tb, err := loadTable(ctx, app, args[0])
			if err != nil {
				return err
			}
			if !app.P.Mode().Interactive {
				return showRows(ctx, app, tb, rowsOptions{limit: 20})
			}
			return runBrowser(ctx, app, snap, tb)
		},
	}
}
