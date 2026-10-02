package main

import (
	"fmt"
	"strings"

	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

func main() {
	m := ui.RichMode(100)
	t := ui.NewTheme(m, nil)
	var blocks []string
	for i, s := range mascot.States {
		blocks = append(blocks, mascot.Portrait(t, s, 0)+"\n\n"+ui.PadRight(t.Muted.Render(s.String()), 15))
		if i%5 == 4 {
			fmt.Println(ui.Columns(4, blocks...))
			fmt.Println()
			blocks = nil
		}
	}
	fmt.Println(mascot.Face(t, mascot.Idle, 0) + "  " + t.Primary.Bold(true).Render("nexus") + strings.Repeat(" ", 2) + mascot.Face(t, mascot.Sleeping, 0))
}
