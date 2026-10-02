package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

// installHelp replaces Cobra's help with Nexus typography.
func installHelp(root *cobra.Command, app *App) {
	render := func(cmd *cobra.Command) string {
		m := app.mode()
		m.JSON = false
		return renderHelp(ui.NewTheme(m, cmd.OutOrStdout()), cmd)
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		fmt.Fprint(cmd.OutOrStdout(), render(cmd))
	})
	root.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprint(cmd.ErrOrStderr(), render(cmd))
		return nil
	})
}

func renderHelp(t *ui.Theme, cmd *cobra.Command) string {
	var b strings.Builder
	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		b.WriteString("\n" + ui.Indent(t.Heading.Render(strings.ToUpper(title)), 2) + "\n")
		b.WriteString(ui.Indent(body, 4) + "\n")
	}

	// Headline.
	b.WriteString("\n")
	if !cmd.HasParent() {
		if face := mascot.Face(t, mascot.Idle, 0); face != "" {
			b.WriteString("  " + face + "  ")
		} else {
			b.WriteString("  ")
		}
		b.WriteString(t.Primary.Bold(true).Render("nexus") + t.Muted.Render(" — "+cmd.Short) + "\n")
	} else {
		b.WriteString("  " + t.ToneGlyph(ui.ToneNexus) + " " + t.Strong.Render(cmd.CommandPath()) + t.Muted.Render(" — "+cmd.Short) + "\n")
	}
	if cmd.Long != "" {
		b.WriteString("\n" + ui.Indent(t.Text.Render(ui.Wrap(cmd.Long, t.Mode.ContentWidth()-6)), 4) + "\n")
	}

	// Usage.
	var usage []string
	if cmd.Runnable() {
		usage = append(usage, t.Code.Render(cmd.UseLine()))
	}
	if cmd.HasAvailableSubCommands() {
		usage = append(usage, t.Code.Render(cmd.CommandPath()+" <command>"))
	}
	section("usage", strings.Join(usage, "\n"))

	if cmd.Example != "" {
		var ex []string
		for _, l := range strings.Split(cmd.Example, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "#") {
				ex = append(ex, t.Muted.Render(l))
			} else if l != "" {
				ex = append(ex, t.Text.Render(l))
			}
		}
		section("examples", strings.Join(ex, "\n"))
	}

	// Subcommands, grouped when groups exist.
	if cmd.HasAvailableSubCommands() {
		width := 0
		for _, c := range cmd.Commands() {
			if c.IsAvailableCommand() && len(c.Name()) > width {
				width = len(c.Name())
			}
		}
		line := func(c *cobra.Command) string {
			return t.Secondary.Render(ui.PadRight(c.Name(), width+3)) + t.Text.Render(c.Short)
		}
		if groups := cmd.Groups(); len(groups) > 0 {
			for _, g := range groups {
				var lines []string
				for _, c := range cmd.Commands() {
					if c.GroupID == g.ID && c.IsAvailableCommand() {
						lines = append(lines, line(c))
					}
				}
				section(g.Title, strings.Join(lines, "\n"))
			}
		} else {
			var lines []string
			for _, c := range cmd.Commands() {
				if c.IsAvailableCommand() {
					lines = append(lines, line(c))
				}
			}
			section("commands", strings.Join(lines, "\n"))
		}
	}

	section("flags", flagTable(t, cmd.LocalNonPersistentFlags()))
	if !cmd.HasParent() {
		section("global flags", flagTable(t, cmd.PersistentFlags()))
	} else {
		var names []string
		cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				names = append(names, "--"+f.Name)
			}
		})
		if len(names) > 0 {
			section("global flags", t.Muted.Render(ui.Wrap(strings.Join(names, "  "), t.Mode.ContentWidth()-6)))
		}
	}

	b.WriteString("\n")
	if cmd.HasAvailableSubCommands() {
		b.WriteString(t.HintString("run "+cmd.CommandPath()+" <command> --help for details") + "\n\n")
	}
	return b.String()
}

func flagTable(t *ui.Theme, fs *pflag.FlagSet) string {
	type row struct{ name, usage string }
	var rows []row
	width := 0
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "    --" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", --" + f.Name
		}
		if typ := f.Value.Type(); typ != "bool" {
			name += " " + typeName(typ)
		}
		usage := f.Usage
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			usage += t.Muted.Render(" (default " + f.DefValue + ")")
		}
		if len(name) > width {
			width = len(name)
		}
		rows = append(rows, row{name, usage})
	})
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = t.Secondary.Render(ui.PadRight(r.name, width+3)) + t.Text.Render(r.usage)
	}
	return strings.Join(lines, "\n")
}

func typeName(typ string) string {
	switch typ {
	case "stringSlice", "stringArray":
		return "strings"
	case "int", "int32", "int64":
		return "n"
	default:
		return typ
	}
}
