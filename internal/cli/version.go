package cli

import (
	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/version"
)

func newVersionCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "show version information",
		Example: "  nexus version\n  nexus version --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			if app.P.Mode().JSON {
				return app.P.JSON(info)
			}
			t := app.T()
			app.P.Say(ui.ToneNexus, "nexus "+info.Version, t.KV(ui.KV{Pairs: [][2]string{
				{"commit", info.Commit},
				{"built", info.Date},
				{"go", info.Go},
				{"platform", info.OS + "/" + info.Arch},
			}}))
			return nil
		},
	}
}
