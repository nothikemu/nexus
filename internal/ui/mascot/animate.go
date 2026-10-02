package mascot

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nothikemu/nexus/internal/ui"
)

// FrameInterval is the default animation cadence.
const FrameInterval = 110 * time.Millisecond

// Step is one segment of an animated sequence.
type Step struct {
	State  State
	Frames int
}

// Play draws a multi-line animation in place. render returns the content for
// state s at frame n; every frame must have the same number of lines.
// The final frame stays on screen. Play returns early if ctx is cancelled.
func Play(ctx context.Context, w io.Writer, steps []Step, interval time.Duration, render func(s State, n int) string) {
	if interval <= 0 {
		interval = FrameInterval
	}
	height := 0
	first := true
	fmt.Fprint(w, "\x1b[?25l")
	defer fmt.Fprint(w, "\x1b[?25h")
	for _, st := range steps {
		for n := 0; n < st.Frames; n++ {
			out := render(st.State, n)
			if !first {
				fmt.Fprintf(w, "\x1b[%dF", height)
			}
			lines := strings.Split(out, "\n")
			for _, l := range lines {
				fmt.Fprint(w, "\x1b[2K"+l+"\n")
			}
			height = len(lines)
			first = false
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}
}

// Wake is the sequence used when Nexus starts: asleep, a stir, then awake.
var Wake = []Step{{Sleeping, 6}, {Idle, 2}, {Connecting, 8}, {Idle, 3}}

// Static returns the content for a single frame, used when animation is off.
func Static(s State, render func(s State, n int) string) string {
	return render(s, 0)
}

// CanAnimate reports whether the theme's mode allows animation.
func CanAnimate(t *ui.Theme) bool {
	return t.Mode.Animate && t.Mode.Unicode && !t.Mode.Plain
}
