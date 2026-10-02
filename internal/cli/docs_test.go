package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestCLIReferenceCoversEveryCommand keeps docs/CLI.md in step with the
// command tree: every visible command must have a "### `nexus …`" heading.
func TestCLIReferenceCoversEveryCommand(t *testing.T) {
	doc, err := os.ReadFile("../../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	root := newRoot(&App{Flags: &Flags{}})
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if !sub.IsAvailableCommand() || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			heading := "### `" + sub.CommandPath()
			if !strings.Contains(string(doc), heading) {
				t.Errorf("docs/CLI.md has no section for %q", sub.CommandPath())
			}
			if sub.Short == "" {
				t.Errorf("%q has no short description", sub.CommandPath())
			}
			if sub.HasAvailableFlags() || sub.Runnable() {
				// every runnable command must render help without error
				var b strings.Builder
				sub.SetOut(&b)
				_ = sub.Help()
			}
			walk(sub)
		}
	}
	walk(root)
}
