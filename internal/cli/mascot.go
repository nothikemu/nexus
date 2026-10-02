package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

func newMascotCmd(app *App) *cobra.Command {
	var loop bool
	cmd := &cobra.Command{
		Use:   "mascot [state]",
		Short: "meet the nexus core",
		Long: "The Nexus core is the small data-being that lives in your terminal: a glowing core " +
			"with a spark above it and links reaching out to two nodes. It shows how Nexus is feeling — " +
			"thinking, working, pleased, worried or asleep.",
		Example: "  nexus mascot\n  nexus mascot celebrating\n  nexus mascot thinking --loop",
		Args:    cobra.MaximumNArgs(1),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			var names []string
			for _, s := range mascot.States {
				names = append(names, s.String())
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			t := app.T()
			if app.P.Mode().JSON {
				type state struct {
					Name    string `json:"name"`
					Caption string `json:"caption"`
				}
				var out []state
				for _, s := range mascot.States {
					out = append(out, state{s.String(), s.Caption()})
				}
				return app.P.JSON(out)
			}
			if len(args) == 1 {
				s, ok := mascot.Parse(args[0])
				if !ok {
					var names []string
					for _, s := range mascot.States {
						names = append(names, s.String())
					}
					return &ui.Problem{Title: "the core has no " + args[0] + " mood.", Detail: "try one of: " + strings.Join(names, ", "), Exit: ui.ExitUsage}
				}
				return showState(cmd.Context(), app, s, loop)
			}
			if mascot.Portrait(t, mascot.Idle, 0) == "" {
				app.P.Say(ui.ToneNexus, "the nexus core", "it lives in your terminal, but plain mode keeps it out of sight.")
				return nil
			}
			hero := func(s mascot.State, n int) string {
				return ui.Indent(mascot.Hero(t, s, n, "the nexus core — it lives here now.", 30), 2)
			}
			app.P.Line("")
			if mascot.CanAnimate(t) {
				mascot.Play(cmd.Context(), app.P.Out, mascot.Wake, mascot.FrameInterval, hero)
			} else {
				app.P.Line(hero(mascot.Idle, 0))
			}
			app.P.Block(gallery(t))
			return nil
		},
	}
	cmd.Flags().BoolVar(&loop, "loop", false, "keep animating until interrupted")
	return cmd
}

// gallery lays out every expression in rows of five.
func gallery(t *ui.Theme) string {
	var rows []string
	var row []string
	for i, s := range mascot.States {
		cell := mascot.Portrait(t, s, 0) + "\n\n" + ui.Center(t.Muted.Render(s.String()), mascot.Width)
		row = append(row, cell)
		if len(row) == 5 || i == len(mascot.States)-1 {
			rows = append(rows, ui.Indent(ui.Columns(3, row...), 2))
			row = nil
		}
	}
	return strings.Join(rows, "\n\n")
}

func showState(ctx context.Context, app *App, s mascot.State, loop bool) error {
	t := app.T()
	render := func(st mascot.State, n int) string {
		return ui.Indent(mascot.Portrait(t, st, n)+"\n\n"+t.Text.Render(st.Caption()), 3)
	}
	app.P.Line("")
	if !mascot.CanAnimate(t) || mascot.Loop(s) == 1 && !loop {
		app.P.Block(render(s, 0))
		return nil
	}
	frames := mascot.Loop(s) * 3
	if loop {
		frames = 1 << 30
	}
	mascot.Play(ctx, app.P.Out, []mascot.Step{{State: s, Frames: frames}}, mascot.FrameInterval, render)
	app.P.Line("")
	return nil
}
