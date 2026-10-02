package dashboard

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/render"
	"github.com/nothikemu/nexus/internal/tui/tuikit"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

func (m *model) View() string {
	if m.browser != nil {
		return m.browser.View()
	}
	t := m.t
	w := m.width
	bodyH := m.height - 5
	if bodyH < 4 {
		bodyH = 4
	}
	var body string
	switch {
	case m.cur == nil:
		body = "\n  " + tuikit.Spark(t, m.frame) + " " + t.Shimmer("nexus is connecting…", m.frame)
	case !m.online():
		body = m.offlineView(bodyH)
	default:
		switch m.tab {
		case tabOverview:
			body = m.overviewView()
		case tabTables:
			body = m.tablesView(bodyH)
		case tabMigrations:
			body = m.migrationsView(bodyH)
		case tabActivity:
			body = m.activityView(bodyH)
		}
	}
	return strings.Join([]string{
		m.headerView(),
		m.tabsView(),
		t.Rule(w),
		tuikit.FitLines(tuikit.Clip(body, w), bodyH),
		t.Rule(w),
		m.footerView(),
	}, "\n")
}

func (m *model) state() mascot.State {
	switch {
	case m.waking:
		return mascot.Connecting
	case m.cur == nil:
		return mascot.Connecting
	case !m.online():
		return mascot.Sleeping
	case len(m.cur.findings) > 0 || (m.cur.migration != nil && !m.cur.migration.UpToDate()):
		return mascot.Curious
	}
	return mascot.Idle
}

func (m *model) headerView() string {
	t := m.t
	face := mascot.Face(t, m.state(), m.frame/2)
	if face == "" {
		face = mascot.Glyph(t, m.state())
	}
	ctx := m.src.Env
	if m.src.Project != "" {
		ctx = m.src.Project + " " + t.Glyphs.Sep + " " + m.src.Env
	}
	if m.src.Protected {
		ctx += " " + t.Warning.Render("protected")
	}
	left := " " + face + "  " + t.Wordmark() + "   " + t.Text.Render(ctx)
	badge := t.Badge(t.Glyphs.Dot+" online", t.Success)
	switch {
	case m.waking:
		badge = t.Badge(t.Glyphs.DotOff+" waking", t.Primary)
	case m.cur != nil && !m.online():
		badge = t.Badge(t.Glyphs.DotOff+" offline", t.Muted)
	case m.cur == nil:
		badge = ""
	}
	gap := m.width - ui.Width(left) - ui.Width(badge) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + badge + " "
}

func (m *model) tabsView() string {
	t := m.t
	var parts []string
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if tab(i) == m.tab {
			parts = append(parts, t.Primary.Bold(true).Render("▸ "+label))
		} else {
			parts = append(parts, t.Muted.Render("  "+label))
		}
	}
	return " " + strings.Join(parts, "   ")
}

func (m *model) footerView() string {
	t := m.t
	var line string
	switch {
	case m.waking:
		line = tuikit.Spark(t, m.frame) + " " + t.Shimmer("nexus is waking up…", m.frame)
	case m.wakeErr != nil:
		line = t.ToneGlyph(ui.ToneError) + " " + t.Error.Render(ui.Truncate(m.wakeErr.Error(), 60, "…"))
	case m.cur == nil:
		line = ""
	case !m.online():
		line = mascot.Glyph(t, mascot.Sleeping) + " " + t.Muted.Render(ui.SayFirst(ui.MomentAsleep))
	case m.state() == mascot.Curious:
		line = mascot.Glyph(t, mascot.Curious) + " " + t.Text.Render(ui.SayFirst(ui.MomentNoticed))
	default:
		line = mascot.Glyph(t, mascot.Idle) + " " + t.Text.Render("nexus is watching.")
	}
	pairs := []string{"1-4", "views"}
	switch {
	case m.tab == tabTables && m.online():
		pairs = append(pairs, "↑↓", "select", "enter", "browse")
	case m.tab != tabOverview && m.online():
		pairs = append(pairs, "↑↓", "select")
	}
	if !m.online() && m.src.Wake != nil && m.cur != nil {
		pairs = append(pairs, "w", "wake")
	}
	pairs = append(pairs, "r", "refresh", "q", "quit")
	hints := tuikit.Hints(t, pairs...)
	gap := m.width - ui.Width(line) - ui.Width(hints) - 3
	if gap < 2 {
		return " " + line
	}
	return " " + line + strings.Repeat(" ", gap) + hints + " "
}

func (m *model) offlineView(h int) string {
	t := m.t
	state := mascot.Sleeping
	caption := ui.SayFirst(ui.MomentAsleep)
	hint := "start it with nexus dev"
	if m.src.Wake != nil {
		hint = "press w to wake it"
	}
	if m.waking {
		state, caption, hint = mascot.Connecting, "waking up…", ""
	}
	hero := mascot.Hero(t, state, m.frame/2, caption, m.width)
	lines := []string{"", hero}
	if hint != "" {
		lines = append(lines, "", ui.Center(t.Code.Render(hint), m.width))
	}
	if m.cur != nil && m.cur.err != nil && !m.waking {
		why := m.cur.err.Error()
		var pr *ui.Problem
		if errors.As(m.cur.err, &pr) && pr.Detail != "" {
			why = pr.Detail
		}
		lines = append(lines, "", ui.Center(t.Faint.Render(ui.Truncate(firstLine(why), m.width-8, "…")), m.width))
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (m *model) overviewView() string {
	t := m.t
	o := m.cur.overview
	snap := m.cur.snap

	// Left: database, migrations, health.
	var left []string
	db := []string{
		t.Strong.Render("PostgreSQL "+o.Version) + t.Muted.Render(runtimeLabel(t, m.src.Runtime)),
		t.Text.Render(o.Database) + t.Muted.Render(" "+t.Glyphs.Sep+" "+ui.Bytes(o.Size)),
	}
	if snap != nil {
		views := 0
		for _, tb := range snap.Tables {
			if tb.IsView() {
				views++
			}
		}
		db = append(db, t.Text.Render(t.JoinSep(ui.Plural(int64(len(snap.BaseTables())), "table", "tables"), ui.Plural(int64(snap.IndexCount()), "index", "indexes"), ui.Plural(int64(views), "view", "views"))))
	}
	cache := "no reads yet"
	if o.CacheHit >= 0 {
		cache = "cache hit " + ui.Percent(o.CacheHit)
	}
	db = append(db, t.Muted.Render(cache+" "+t.Glyphs.Sep+" up "+ui.Duration(time.Since(o.StartedAt))))
	left = append(left, t.Section("database", strings.Join(db, "\n")))

	if st := m.cur.migration; st != nil {
		sum, tone := render.MigrationSummary(t, st)
		lines := []string{t.Tone(tone).Render(t.Glyphs.Dot) + " " + t.Text.Render(sum)}
		if l := st.Latest(); l != nil {
			lines = append(lines, t.Muted.Render("last "+l.Name+" "+t.Glyphs.Sep+" "+ui.Ago(*l.AppliedAt, time.Now())))
		}
		left = append(left, t.Section("migrations", strings.Join(lines, "\n")))
	}
	if len(m.cur.findings) == 0 {
		left = append(left, t.Section("health", t.Success.Render(t.Glyphs.Insight)+" "+t.Text.Render("no problems")))
	} else {
		var lines []string
		for _, f := range m.cur.findings {
			tone := ui.ToneWarning
			if f.Severity == "info" {
				tone = ui.ToneInfo
			}
			lines = append(lines, t.ToneGlyph(tone)+" "+t.Text.Render(f.Title))
		}
		left = append(left, t.Section("health", strings.Join(lines, "\n")))
	}

	if snap != nil {
		if top := largest(snap.BaseTables(), 4); len(top) > 0 {
			var lines []string
			for _, tb := range top {
				frac := float64(tb.Bytes) / float64(max(top[0].Bytes, 1))
				lines = append(lines, t.Text.Render(ui.PadRight(tb.DisplayName(), 14))+" "+t.Bar(frac, 12, t.Palette.Database)+" "+t.Muted.Render(ui.PadLeft(ui.Bytes(tb.Bytes), 8)))
			}
			left = append(left, t.Section("largest tables", strings.Join(lines, "\n")))
		}
	}

	// Right: throughput and activity.
	spark := func(label string, s *series, value, hex string) string {
		return t.Key.Render(ui.PadRight(label, 9)) + t.Sparkline(s.values, 28, hex) + "  " + t.Text.Render(value)
	}
	rate := func(v float64) string { return ui.Compact(v) + "/s" }
	tp := []string{
		spark("tx", &m.commits, rate(m.commits.last()), t.Palette.Primary),
		spark("reads", &m.reads, rate(m.reads.last()), t.Palette.Secondary),
		spark("writes", &m.writes, rate(m.writes.last()), t.Palette.Spark),
		spark("conns", &m.conns, fmt.Sprintf("%d / %d", o.Connections, o.MaxConnections), t.Palette.Success),
	}
	right := []string{t.Section("throughput", strings.Join(tp, "\n")), t.Section("activity", m.feed(6))}

	leftCol := strings.Join(left, "\n\n")
	rightCol := strings.Join(right, "\n\n")
	if m.width < 96 {
		return "\n" + ui.Indent(leftCol+"\n\n"+rightCol, 2)
	}
	return "\n" + ui.Indent(ui.Columns(6, padTo(leftCol, 40), rightCol), 2)
}

// largest returns up to n tables ordered by size.
func largest(tables []*introspect.Table, n int) []*introspect.Table {
	out := append([]*introspect.Table(nil), tables...)
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func runtimeLabel(t *ui.Theme, rt string) string {
	if rt == "" {
		return ""
	}
	return " " + t.Glyphs.Sep + " " + rt
}

func padTo(block string, w int) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = ui.PadRight(l, w)
	}
	return strings.Join(lines, "\n")
}

// feed merges migration events and live queries into one activity list.
func (m *model) feed(n int) string {
	t := m.t
	var lines []string
	for _, s := range m.cur.sessions {
		if len(lines) >= n {
			break
		}
		q := strings.Join(strings.Fields(s.Query), " ")
		lines = append(lines, t.Success.Render(t.Glyphs.Dot)+" "+t.Muted.Render(ui.PadRight(s.State, 7))+" "+t.Text.Render(ui.Truncate(q, 44, "…"))+t.Faint.Render(" "+fmt.Sprintf("%.1fs", s.Duration)))
	}
	for _, e := range m.cur.events {
		if len(lines) >= n {
			break
		}
		verb := map[string]string{"apply": "applied", "rollback": "rolled back", "repair": "repaired"}[e.Action]
		lines = append(lines, t.Faint.Render(e.At.Local().Format("15:04"))+"  "+t.Success.Render(t.Glyphs.Insight)+" "+t.Text.Render("migration "+verb)+"  "+t.Muted.Render(e.Name))
	}
	if len(lines) == 0 {
		return t.Muted.Render("quiet. no queries running.")
	}
	return strings.Join(lines, "\n")
}

func (m *model) tablesView(h int) string {
	t := m.t
	snap := m.cur.snap
	if snap == nil || len(snap.Tables) == 0 {
		return "\n  " + t.Muted.Render("no tables yet — create one with a migration.")
	}
	sel := min(m.selected[tabTables], len(snap.Tables)-1)
	list := windowed(snap.Tables, sel, h-3)
	out := ui.Table{Indent: 1, Columns: []ui.Column{{Title: ""}, {Title: "name"}, {Title: "kind"}, {Title: "rows", Align: ui.AlignRight}, {Title: "size", Align: ui.AlignRight}, {Title: "columns", Align: ui.AlignRight}, {Title: "", Flex: true}}}
	for _, it := range list {
		tb := it.v
		marker := " "
		name := t.Text.Render(tb.DisplayName())
		if it.i == sel {
			marker = t.Primary.Bold(true).Render("▌")
			name = t.Primary.Bold(true).Render(tb.DisplayName())
		}
		var notes []string
		if len(tb.PrimaryKey) == 0 && !tb.IsView() {
			notes = append(notes, t.Warning.Render("no primary key"))
		}
		if tb.RLSEnabled {
			notes = append(notes, t.Muted.Render("rls"))
		}
		if tb.Comment != "" {
			notes = append(notes, t.Faint.Render(tb.Comment))
		}
		out.Rows = append(out.Rows, []string{marker, name, t.Muted.Render(tb.Kind), t.Number.Render(ui.Count(tb.Rows)), t.Muted.Render(ui.Bytes(tb.Bytes)), t.Text.Render(fmt.Sprint(len(tb.Columns))), strings.Join(notes, t.Faint.Render(" · "))})
	}
	return "\n" + t.Table(out)
}

func (m *model) migrationsView(h int) string {
	t := m.t
	st := m.cur.migration
	if st == nil {
		return "\n  " + t.Muted.Render("migrations are managed per project — open nexus inside one.")
	}
	sum, tone := render.MigrationSummary(t, st)
	head := " " + t.ToneGlyph(tone) + " " + t.Tone(tone).Bold(true).Render(sum)
	entries := st.Entries
	sel := min(m.selected[tabMigrations], max(len(entries)-1, 0))
	var win []migrate.Entry
	for _, it := range windowed(entries, sel, h-4) {
		win = append(win, it.v)
	}
	body := render.MigrationStatus(t, &migrate.Status{Entries: win})
	return "\n" + head + "\n\n" + body
}

func (m *model) activityView(h int) string {
	t := m.t
	if len(m.cur.sessions) == 0 {
		return "\n  " + t.Muted.Render("quiet. no other sessions are running queries.") + "\n\n" + ui.Indent(t.Section("recent", m.feed(h-6)), 2)
	}
	sel := min(m.selected[tabActivity], len(m.cur.sessions)-1)
	out := ui.Table{Indent: 1, Columns: []ui.Column{{Title: ""}, {Title: "pid", Align: ui.AlignRight}, {Title: "user"}, {Title: "app"}, {Title: "state"}, {Title: "for", Align: ui.AlignRight}, {Title: "query", Flex: true}}}
	for _, it := range windowed(m.cur.sessions, sel, h-3) {
		s := it.v
		marker := " "
		if it.i == sel {
			marker = t.Primary.Bold(true).Render("▌")
		}
		state := t.Muted.Render(s.State)
		if s.State == "active" {
			state = t.Success.Render(s.State)
		}
		if s.WaitEvent != "" {
			state += t.Warning.Render(" " + s.WaitEvent)
		}
		out.Rows = append(out.Rows, []string{marker, t.Muted.Render(fmt.Sprint(s.PID)), t.Text.Render(s.User), t.Muted.Render(s.Application), state,
			t.Text.Render(fmt.Sprintf("%.1fs", s.Duration)), t.Text.Render(strings.Join(strings.Fields(s.Query), " "))})
	}
	return "\n" + t.Table(out)
}

type indexed[T any] struct {
	i int
	v T
}

// windowed returns at most n items around the selection.
func windowed[T any](items []T, sel, n int) []indexed[T] {
	if n < 1 {
		n = 1
	}
	start := 0
	if sel >= n {
		start = sel - n + 1
	}
	end := min(len(items), start+n)
	out := make([]indexed[T], 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, indexed[T]{i, items[i]})
	}
	return out
}
