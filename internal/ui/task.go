package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// spinnerFrames is the Nexus twinkle: the spark brightening and fading.
var spinnerFrames = []string{"·", "✧", "✦", "✦", "✧", "·"}

const spinnerInterval = 90 * time.Millisecond

// Task is a unit of visible work: an animated twinkle while running (on
// stderr, only on terminals), then a single settled line on stdout:
//
//	✓ PostgreSQL          16.14 · native · 127.0.0.1:54320     1.2s
type Task struct {
	p      *Printer
	label  string
	indent int
	start  time.Time

	mu     sync.Mutex
	status string
	stop   chan struct{}
	done   chan struct{}
	ended  bool
}

// TaskLabelWidth aligns task details into a column.
const TaskLabelWidth = 18

// Task starts a task. Call exactly one of Done, Warn, Fail or Skip.
func (p *Printer) Task(label string) *Task {
	return p.TaskIndent(label, 2)
}

// TaskIndent starts a task rendered at the given indent.
func (p *Printer) TaskIndent(label string, indent int) *Task {
	t := &Task{p: p, label: label, indent: indent, start: time.Now()}
	if p.T.Mode.Animate && p.T.Mode.StderrTTY && !p.Quiet() {
		t.stop = make(chan struct{})
		t.done = make(chan struct{})
		go t.spin()
	}
	return t
}

// Update changes the in-progress status text shown next to the spinner.
func (t *Task) Update(status string) {
	t.mu.Lock()
	t.status = status
	t.mu.Unlock()
}

// Elapsed is the time since the task started.
func (t *Task) Elapsed() time.Duration { return time.Since(t.start) }

// Done settles the task as successful.
func (t *Task) Done(detail string) { t.settle(t.p.T.Success.Render(t.p.T.Glyphs.Check), detail, true) }

// Warn settles the task with a warning.
func (t *Task) Warn(detail string) {
	t.settle(t.p.T.Warning.Render(t.p.T.Glyphs.Warning), detail, true)
}

// Fail settles the task as failed.
func (t *Task) Fail(detail string) { t.settle(t.p.T.Error.Render(t.p.T.Glyphs.Cross), detail, true) }

// Clear stops the spinner without printing a settled line.
func (t *Task) Clear() {
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return
	}
	t.ended = true
	t.mu.Unlock()
	if t.stop != nil {
		close(t.stop)
		<-t.done
	}
}

// Skip settles the task as skipped (no timing shown).
func (t *Task) Skip(detail string) { t.settle(t.p.T.Muted.Render(t.p.T.Glyphs.Skip), detail, false) }

func (t *Task) settle(glyph, detail string, timed bool) {
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return
	}
	t.ended = true
	t.mu.Unlock()
	if t.stop != nil {
		close(t.stop)
		<-t.done
	}
	if t.p.Quiet() {
		return
	}
	th := t.p.T
	line := strings.Repeat(" ", t.indent) + glyph + " " + th.Text.Render(PadRight(t.label, TaskLabelWidth))
	if detail != "" {
		line += "  " + th.Muted.Render(detail)
	}
	// Only meaningful durations are shown; sub-100ms timings are noise.
	if el := time.Since(t.start); timed && el >= 100*time.Millisecond {
		line += "  " + th.Faint.Render(Duration(el))
	}
	t.p.Line(line)
}

func (t *Task) spin() {
	defer close(t.done)
	w := t.p.Err
	fmt.Fprint(w, "\x1b[?25l") // hide cursor
	defer fmt.Fprint(w, "\r\x1b[2K\x1b[?25h")
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	frame := 0
	for {
		t.render(frame)
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			frame++
		}
	}
}

func (t *Task) render(frame int) {
	th := t.p.T
	t.mu.Lock()
	status := t.status
	t.mu.Unlock()

	glyph := th.Primary.Bold(true).Render(spinnerFrames[frame%len(spinnerFrames)])
	label := th.Shimmer(t.label, frame)
	line := strings.Repeat(" ", t.indent) + glyph + " " + label
	if status != "" {
		line += "  " + th.Muted.Render(status)
	}
	if el := time.Since(t.start); el > 2*time.Second {
		line += "  " + th.Faint.Render(fmt.Sprintf("%ds", int(el.Seconds())))
	}
	line = Truncate(line, th.Mode.Width-1, "…")
	fmt.Fprint(t.p.Err, "\r\x1b[2K"+line)
}

// Shimmer renders text with a soft highlight sweeping across it — the
// "nexus is thinking" effect. Without colour it returns the text unchanged.
func (t *Theme) Shimmer(text string, frame int) string {
	if !t.Mode.Color {
		return text
	}
	runes := []rune(text)
	n := len(runes)
	period := n + 8
	pos := frame % period
	var b strings.Builder
	for i, r := range runes {
		d := i - pos + 4
		if d < 0 {
			d = -d
		}
		f := 0.0
		if d < 4 {
			f = 1 - float64(d)/4
		}
		col := Blend(t.Palette.Muted, t.Palette.Strong, f)
		b.WriteString(t.Style(col).Render(string(r)))
	}
	return b.String()
}

// RestoreCursor shows the cursor again; safe to call at any time (used when
// a command is interrupted mid-spinner).
func RestoreCursor(p *Printer) {
	if p != nil && p.T.Mode.StderrTTY {
		fmt.Fprint(p.Err, "\x1b[?25h")
	}
}
