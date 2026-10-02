package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/localdb"
	"github.com/nothikemu/nexus/internal/project"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

type initResult struct {
	Project string         `json:"project"`
	Root    string         `json:"root"`
	Created []string       `json:"created"`
	Updated []string       `json:"updated,omitempty"`
	Runtime config.Runtime `json:"runtime"`
	Port    int            `json:"port"`
}

func newInitCmd(app *App) *cobra.Command {
	var (
		blank   bool
		runtime string
		port    int
		pgVer   int
	)
	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "create a new nexus project",
		Long: "Creates a project directory with nexus.yaml, a first migration and seed data. " +
			"Without a name, nexus initialises the current directory.",
		Example: "  nexus init my-app\n  nexus init            # use the current directory\n  nexus init api --blank --runtime docker",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir, name, err := initTarget(app, args)
			if err != nil {
				return err
			}
			rt := config.Runtime(runtime)
			switch rt {
			case "", config.RuntimeAuto, config.RuntimeNative, config.RuntimeDocker:
			default:
				return &ui.Problem{Title: "--runtime must be auto, native or docker.", Hint: "for an existing database, set database.url in nexus.yaml after init", Exit: ui.ExitUsage}
			}
			return runInit(ctx, app, project.ScaffoldOptions{Dir: dir, Name: name, Blank: blank, Runtime: rt, Port: port, Version: pgVer})
		},
	}
	cmd.Flags().BoolVar(&blank, "blank", false, "skip the starter migration and seed data")
	cmd.Flags().StringVar(&runtime, "runtime", "", "local database runtime: auto, native or docker")
	cmd.Flags().IntVar(&port, "port", 0, "local database port (default: first free from 54320)")
	cmd.Flags().IntVar(&pgVer, "pg-version", 0, "PostgreSQL major version for local development (default 16)")
	return cmd
}

var unsafeName = regexp.MustCompile(`[^a-z0-9_-]+`)

// projectName derives a valid project name from a directory name.
func projectName(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	n = unsafeName.ReplaceAllString(n, "-")
	n = strings.Trim(n, "-_")
	if n == "" {
		n = "nexus-app"
	}
	if len(n) > 63 {
		n = n[:63]
	}
	return n
}

func initTarget(app *App, args []string) (dir, name string, err error) {
	if len(args) == 1 {
		dir = args[0]
		name = projectName(filepath.Base(filepath.Clean(dir)))
		if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
			return "", "", &ui.Problem{Title: dir + " is a file.", Exit: ui.ExitUsage}
		}
		return dir, name, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	name, err = app.P.Ask("project name?", projectName(filepath.Base(wd)))
	if err != nil {
		return "", "", err
	}
	return ".", projectName(name), nil
}

func runInit(ctx context.Context, app *App, opts project.ScaffoldOptions) error {
	t := app.T()
	p := app.P

	// The first-run moment: the core wakes up.
	if !p.Quiet() && mascot.Portrait(t, mascot.Idle, 0) != "" {
		hero := func(s mascot.State, n int) string {
			return ui.Indent(mascot.Hero(t, s, n, "your database, right in the terminal.", 40), 2)
		}
		p.Line("")
		if mascot.CanAnimate(t) {
			mascot.Play(ctx, p.Out, mascot.Wake, mascot.FrameInterval, hero)
		} else {
			p.Line(hero(mascot.Idle, 0))
		}
	} else if !p.Quiet() && !t.Mode.Plain {
		p.Block("  " + t.Wordmark() + "\n  " + t.Muted.Render("your database, right in the terminal."))
	}

	p.Say(ui.ToneNexus, "setting up "+opts.Name)
	task := p.TaskIndent("project", 4)
	res, err := project.Scaffold(opts)
	if err != nil {
		task.Fail("")
		return err
	}
	task.Done(config.FileName + " " + t.Glyphs.Sep + " " + res.Config.Migrations.Dir + "/ " + t.Glyphs.Sep + " seeds/")

	task = p.TaskIndent("first migration", 4)
	var migration string
	for _, c := range res.Created {
		if strings.HasPrefix(c, res.Config.Migrations.Dir+string(filepath.Separator)) {
			migration = filepath.Base(c)
		}
	}
	if migration != "" {
		task.Done(migration)
	} else {
		task.Skip("blank project")
	}

	proj, err := project.Load(res.Root)
	if err != nil {
		return err
	}
	task = p.TaskIndent("PostgreSQL", 4)
	kind, reason := localdb.Resolve(ctx, proj)
	rt, _ := localdb.New(ctx, proj)
	if cerr := rt.Check(ctx); cerr != nil {
		task.Warn(string(kind) + " " + t.Glyphs.Sep + " " + checkMessage(cerr))
	} else {
		detail := string(kind) + " " + t.Glyphs.Sep + " " + reason
		if kind == config.RuntimeNative {
			if b := localdb.FindBinaries(ctx); len(b) > 0 {
				detail = "PostgreSQL " + b[0].Version + " " + t.Glyphs.Sep + " native"
			}
		}
		task.Done(detail)
	}

	task = p.TaskIndent("git", 4)
	task.Done(project.StateDirName + "/ is ignored " + t.Glyphs.Sep + " local credentials stay local")

	if p.Mode().JSON {
		return p.JSON(initResult{Project: opts.Name, Root: res.Root, Created: res.Created, Updated: res.Updated, Runtime: kind, Port: res.Config.Database.Port})
	}

	var next []string
	if opts.Dir != "." {
		next = append(next, t.Code.Render("cd "+opts.Dir))
	}
	next = append(next, t.Code.Render("nexus dev")+t.Muted.Render("     start the local database"))
	p.Say(ui.ToneSuccess, ui.SayFirst(ui.MomentReady), strings.Join(next, "\n"))
	return nil
}

func checkMessage(err error) string {
	switch {
	case errors.Is(err, localdb.ErrRoot):
		return "can't run as root — use docker"
	case errors.Is(err, localdb.ErrNoBinaries):
		return "not installed — see nexus doctor"
	case errors.Is(err, localdb.ErrNoDocker):
		return "docker isn't reachable"
	default:
		return err.Error()
	}
}
