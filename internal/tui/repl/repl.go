// Package repl is the interactive Nexus SQL shell: multi-line input that
// runs when a statement is complete, persistent history, cancellable
// queries, meta commands, and results printed into the scrollback.
package repl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/render"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/sqltext"
	"github.com/nothikemu/nexus/internal/tui/tuikit"
	"github.com/nothikemu/nexus/internal/ui"
)

// Options configures the shell.
type Options struct {
	Theme     *ui.Theme
	Pool      *pgxpool.Pool
	Database  string // shown in the prompt
	Env       string
	Protected bool
	History   string // history file path ("" = no persistence)
	Schemas   []string
	RowLimit  int
}

// Run starts the shell and blocks until the user quits.
func Run(ctx context.Context, o Options) error {
	conn, err := o.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if o.RowLimit == 0 {
		o.RowLimit = 500
	}
	m := newModel(ctx, o, conn)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	_, err = p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

type resultMsg struct {
	results []*pg.Result
	err     error
	sql     string
	elapsed time.Duration
}

type metaMsg struct {
	out  string
	quit bool
}

type confirm struct {
	sql    string
	phrase string // what must be typed; "y" for a simple yes
}

type model struct {
	ctx      context.Context
	o        Options
	t        *ui.Theme
	conn     *pgxpool.Conn
	input    textarea.Model
	history  *history
	running  bool
	cancel   context.CancelFunc
	started  time.Time
	frame    int
	expanded bool
	timing   bool
	width    int
	confirm  *confirm
	quitting bool
}

func newModel(ctx context.Context, o Options, conn *pgxpool.Conn) *model {
	t := o.Theme
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 12
	ta.SetHeight(1)
	ta.Placeholder = "select * from …"
	st := textarea.Style{
		Base:        t.R.NewStyle(),
		CursorLine:  t.R.NewStyle(),
		Placeholder: t.Faint,
		Prompt:      t.R.NewStyle(),
		Text:        t.Text,
		EndOfBuffer: t.Faint,
	}
	ta.FocusedStyle, ta.BlurredStyle = st, st
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.KeyMap.LineNext.SetEnabled(false)
	ta.KeyMap.LinePrevious.SetEnabled(false)
	m := &model{ctx: ctx, o: o, t: t, conn: conn, input: ta, history: loadHistory(o.History), timing: true}
	m.input.Focus()
	m.setPrompt()
	return m
}

func (m *model) promptText() string {
	if m.confirm != nil {
		return "confirm " + m.t.Glyphs.Prompt + " "
	}
	return m.o.Database + " " + m.t.Glyphs.Prompt + " "
}

func (m *model) setPrompt() {
	first := m.promptText()
	w := ui.Width(first)
	cont := strings.Repeat(" ", w-2) + m.t.Glyphs.Sep + " "
	style := m.t.Primary.Bold(true)
	if m.confirm != nil {
		style = m.t.Warning.Bold(true)
	}
	m.input.SetPromptFunc(w, func(line int) string {
		if line == 0 {
			return style.Render(first)
		}
		return m.t.Faint.Render(cont)
	})
}

func (m *model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.SetWidth(msg.Width - 1)
		return m, nil

	case tuikit.TickMsg:
		if m.running {
			m.frame++
			return m, tuikit.Tick(90 * time.Millisecond)
		}
		return m, nil

	case resultMsg:
		m.running = false
		m.cancel = nil
		return m, m.printResults(msg)

	case metaMsg:
		var cmds []tea.Cmd
		if msg.out != "" {
			cmds = append(cmds, tea.Println(msg.out+"\n"))
		}
		if msg.quit {
			m.quitting = true
			cmds = append(cmds, tea.Quit)
		}
		return m, tea.Sequence(cmds...)

	case tea.KeyMsg:
		return m.key(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		switch {
		case m.running && m.cancel != nil:
			m.cancel()
			return m, nil
		case m.confirm != nil:
			m.confirm = nil
			m.input.Reset()
			m.setPrompt()
			return m, tea.Println(m.t.Muted.Render("  cancelled — nothing ran.\n"))
		case m.input.Value() != "":
			m.input.Reset()
			m.resize()
			return m, nil
		}
		return m, m.quit()
	case "ctrl+d":
		if m.input.Value() == "" && !m.running {
			return m, m.quit()
		}
	case "ctrl+l":
		return m, tea.ClearScreen
	}
	if m.running {
		return m, nil // keystrokes wait until the query finishes
	}
	switch msg.String() {
	case "up":
		if m.input.Line() == 0 {
			if v, ok := m.history.prev(m.input.Value()); ok {
				m.input.SetValue(v)
				m.resize()
			}
			return m, nil
		}
		m.input.CursorUp()
		return m, nil
	case "down":
		if m.input.Line() == m.input.LineCount()-1 {
			if v, ok := m.history.next(); ok {
				m.input.SetValue(v)
				m.resize()
			}
			return m, nil
		}
		m.input.CursorDown()
		return m, nil
	case "enter":
		return m, m.submit()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.resize()
	return m, cmd
}

func (m *model) resize() {
	h := m.input.LineCount()
	if h < 1 {
		h = 1
	}
	m.input.SetHeight(h)
}

// submit runs the input when it is a complete statement or a meta command;
// otherwise Enter inserts a newline.
func (m *model) submit() tea.Cmd {
	raw := m.input.Value()
	text := strings.TrimSpace(raw)

	if c := m.confirm; c != nil {
		m.confirm = nil
		m.input.Reset()
		m.setPrompt()
		ok := text == c.phrase || (c.phrase == "y" && (text == "yes" || text == "Y"))
		if !ok {
			return tea.Println(m.t.Muted.Render("  cancelled — nothing ran.\n"))
		}
		return m.execute(c.sql)
	}
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, `\`) {
		m.history.add(text)
		m.input.Reset()
		m.resize()
		return tea.Sequence(tea.Println(m.echo(text)), m.meta(text))
	}
	if !sqltext.Complete(raw) {
		m.input.InsertString("\n")
		m.resize()
		return nil
	}
	m.history.add(text)
	m.input.Reset()
	m.resize()
	echo := tea.Println(m.echo(text))

	if m.o.Protected {
		if level, _ := safety.ClassifySQL(text); level > safety.ReadOnly {
			phrase := "y"
			warn := "this changes data on " + m.o.Env + "."
			if level == safety.Destructive {
				phrase = safety.Phrase(m.o.Env)
				warn = "this may permanently delete data on " + m.o.Env + "."
			}
			m.confirm = &confirm{sql: text, phrase: phrase}
			m.setPrompt()
			ask := "  " + m.t.ToneGlyph(ui.ToneWarning) + " " + m.t.Warning.Render(warn) + "\n  " +
				m.t.Muted.Render("type ") + m.t.Warning.Bold(true).Render(phrase) + m.t.Muted.Render(" to run it, anything else to cancel.")
			return tea.Sequence(echo, tea.Println(ask))
		}
	}
	return tea.Sequence(echo, m.execute(text))
}

// echo renders the submitted input as it should remain in the scrollback.
func (m *model) echo(text string) string {
	first := m.t.Primary.Bold(true).Render(m.promptText())
	w := ui.Width(m.promptText())
	cont := m.t.Faint.Render(strings.Repeat(" ", w-2) + m.t.Glyphs.Sep + " ")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = first + m.t.Text.Render(l)
		} else {
			lines[i] = cont + m.t.Text.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) execute(sql string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.running = true
	m.cancel = cancel
	m.started = time.Now()
	m.frame = 0
	conn := m.conn.Conn().PgConn()
	limit := m.o.RowLimit
	run := func() tea.Msg {
		defer cancel()
		start := time.Now()
		res, err := pg.ExecScript(ctx, conn, sql, limit)
		if ctx.Err() != nil && err != nil {
			err = context.Canceled
		}
		return resultMsg{results: res, err: err, sql: sql, elapsed: time.Since(start)}
	}
	return tea.Batch(run, tuikit.Tick(90*time.Millisecond))
}

func (m *model) printResults(msg resultMsg) tea.Cmd {
	t := m.t
	var out []string
	for _, r := range msg.results {
		if r.HasRows() {
			block := ui.Indent(render.Result(t, r, m.expanded), 2)
			if m.timing {
				block += "\n\n" + ui.Indent(render.Footer(t, r), 2)
			}
			out = append(out, block)
		} else {
			line := "  " + t.ToneGlyph(ui.ToneSuccess) + " " + t.Text.Render(render.HumanTag(r.Command))
			if m.timing {
				line += "  " + t.Muted.Render(ui.Duration(r.Duration))
			}
			out = append(out, line)
		}
	}
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			out = append(out, "  "+t.Muted.Render("cancelled after "+ui.Duration(msg.elapsed)+"."))
		} else {
			var pos int32
			if pe, ok := pg.AsPgError(msg.err); ok {
				pos = pe.Position
			}
			pr := render.SQLProblem(t, msg.err, msg.sql, 0, pos)
			out = append(out, ui.Indent(t.ProblemString(pr), 2))
		}
		if m.inFailedTransaction() {
			out = append(out, "  "+t.Muted.Render("the transaction is aborted — run rollback; to continue."))
		}
	}
	if len(out) == 0 {
		return nil
	}
	lead := ""
	if len(msg.results) > 0 && msg.results[0].HasRows() {
		lead = "\n" // tables get a breath of space under the statement
	}
	return tea.Println(lead + strings.Join(out, "\n\n") + "\n")
}

// inFailedTransaction reports whether the session is inside an aborted transaction.
func (m *model) inFailedTransaction() bool {
	return m.conn.Conn().PgConn().TxStatus() == 'E'
}

func (m *model) quit() tea.Cmd {
	m.quitting = true
	return tea.Sequence(tea.Println(m.t.Muted.Render("  "+ui.Say(ui.MomentGoodbye))), tea.Quit)
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	view := m.input.View()
	if m.running {
		el := time.Since(m.started)
		status := tuikit.Spark(m.t, m.frame) + " " + m.t.Shimmer("nexus is thinking…", m.frame)
		if el > time.Second {
			status += "  " + m.t.Faint.Render(fmt.Sprintf("%.0fs", el.Seconds()))
		}
		status += "  " + m.t.Faint.Render("ctrl+c cancels")
		view += "\n" + status
	}
	return view
}
