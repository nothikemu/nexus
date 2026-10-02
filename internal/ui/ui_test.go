package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func plain() *Theme { return NewTheme(PlainMode(), &bytes.Buffer{}) }
func rich() *Theme  { return NewTheme(RichMode(100), &bytes.Buffer{}) }
func nocolor() *Theme {
	m := RichMode(100)
	m.Color = false
	return NewTheme(m, &bytes.Buffer{})
}

func TestModesStripDecoration(t *testing.T) {
	for _, th := range []*Theme{plain(), nocolor()} {
		out := th.SayString(ToneSuccess, "nice.", "body") + th.Panel(Panel{Title: "t", Body: "x"}) + th.Sparkline([]float64{1, 2, 3}, 5, "")
		if strings.Contains(out, "\x1b[") {
			t.Errorf("colour escape in %+v output: %q", th.Mode, out)
		}
	}
	if !strings.Contains(rich().SayString(ToneNexus, "hi"), "\x1b[") {
		t.Error("rich mode should colour output")
	}
	if g := plain().Glyphs; g.Nexus != "*" || g.TreeMid != "|-" {
		t.Errorf("plain glyphs = %+v", g)
	}
}

func TestSayLayout(t *testing.T) {
	got := plain().SayString(ToneNexus, "nexus is awake.", "Connected to PostgreSQL\n14 tables", "ready when you are.")
	want := "* nexus is awake.\n\n  Connected to PostgreSQL\n  14 tables\n\n  ready when you are."
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPanelGeometry(t *testing.T) {
	th := nocolor()
	out := th.Panel(Panel{Title: "my-app", Right: "online", Body: "line one\nline two that is longer"})
	lines := strings.Split(out, "\n")
	w := Width(lines[0])
	for i, l := range lines {
		if Width(l) != w {
			t.Errorf("line %d width %d != %d: %q", i, Width(l), w, l)
		}
	}
	if !strings.HasPrefix(lines[0], "╭─ my-app ") || !strings.HasSuffix(lines[0], " online ─╮") {
		t.Errorf("top border = %q", lines[0])
	}
	if len(lines) != 6 { // top, pad, 2 body, pad, bottom
		t.Errorf("panel has %d lines", len(lines))
	}
	if p := plain().Panel(Panel{Title: "t", Body: "x"}); p != "T\n  x" {
		t.Errorf("plain panel = %q", p)
	}
}

func TestTableShrinksToWidth(t *testing.T) {
	th := plain()
	tb := Table{Width: 30, Columns: []Column{{Title: "name"}, {Title: "notes", Flex: true}, {Title: "n", Align: AlignRight}},
		Rows: [][]string{{"users", strings.Repeat("long text ", 10), "3"}, {"posts", "short", "50000"}}}
	out := th.Table(tb)
	for _, l := range strings.Split(out, "\n") {
		if Width(l) > 30 {
			t.Errorf("line exceeds width: %d %q", Width(l), l)
		}
	}
	if !strings.Contains(out, "…") {
		t.Error("expected truncation")
	}
	if !strings.Contains(out, "50000") || !strings.Contains(out, "    3") {
		t.Errorf("numeric column not right-aligned:\n%s", out)
	}
}

func TestTreeAndKV(t *testing.T) {
	th := nocolor()
	tree := th.Tree([]TreeNode{{Label: "public", Children: []TreeNode{{Label: "users"}, {Label: "posts"}}}, {Label: "audit"}})
	want := "├─ public\n│  ├─ users\n│  └─ posts\n└─ audit"
	if tree != want {
		t.Errorf("tree:\n%s\nwant:\n%s", tree, want)
	}
	kv := th.KV(KV{Pairs: [][2]string{{"size", "1 MB"}, {"connections", "2"}}})
	if kv != "size          1 MB\nconnections   2" {
		t.Errorf("kv = %q", kv)
	}
}

func TestSparklineAndBar(t *testing.T) {
	th := nocolor()
	s := th.Sparkline([]float64{0, 5, 10}, 5, "")
	if s != "▁▁▁▅█" {
		t.Errorf("sparkline = %q", s)
	}
	if b := th.Bar(0.5, 4, ""); b != "██░░" {
		t.Errorf("bar = %q", b)
	}
	if b := plain().Bar(0.5, 4, ""); b != "[##..]" {
		t.Errorf("plain bar = %q", b)
	}
}

func TestCodeFrame(t *testing.T) {
	th := nocolor()
	src := "create table a (\n  id integr\n);"
	pos := strings.Index(src, "integr") + 1
	frame := th.CodeFrame(src, pos, 10, "")
	want := "11 │ create table a (\n12 │   id integr\n   │      ^"
	if frame != want {
		t.Errorf("frame:\n%s\nwant:\n%s", frame, want)
	}
}

func TestNumberFormatting(t *testing.T) {
	cases := map[string]string{
		Count(184291):                     "184,291",
		Count(-1000):                      "-1,000",
		Compact(1234):                     "1.2k",
		Compact(2_000_000):                "2M",
		Bytes(512):                        "512 B",
		Bytes(2_936_012):                  "2.8 MB",
		Bytes(48 * 1024):                  "48 KB",
		Percent(0.9996):                   "99.9%",
		Duration(1500 * time.Millisecond): "1.5s",
		Duration(90 * time.Minute):        "1h 30m",
		Plural(1, "table", "tables"):      "1 table",
		Plural(3, "table", "tables"):      "3 tables",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestPrinterSpacing(t *testing.T) {
	var out, errb bytes.Buffer
	p := NewTestPrinter(PlainMode(), &out, &errb)
	p.Say(ToneNexus, "one")
	p.Say(ToneSuccess, "two")
	p.Hint("next")
	if got := out.String(); got != "* one\n\n+ two\n\n  -> next\n" {
		t.Errorf("output = %q", got)
	}
	var jout bytes.Buffer
	jp := NewTestPrinter(Mode{JSON: true}, &jout, &errb)
	jp.Say(ToneNexus, "hidden")
	_ = jp.JSON(map[string]int{"a": 1})
	if jout.String() != "{\n  \"a\": 1\n}\n" {
		t.Errorf("json mode leaked human output: %q", jout.String())
	}
}

func TestTaskPlain(t *testing.T) {
	var out bytes.Buffer
	p := NewTestPrinter(PlainMode(), &out, &bytes.Buffer{})
	p.Task("PostgreSQL").Done("16.4")
	p.Task("seed").Skip("nothing")
	got := ansi.Strip(out.String())
	if !strings.Contains(got, "ok PostgreSQL") || !strings.Contains(got, "16.4") || !strings.Contains(got, "- seed") {
		t.Errorf("task output = %q", got)
	}
}

func TestVoiceIsStable(t *testing.T) {
	if Say(MomentAwake) == "" || SayFirst(MomentAwake) != "nexus is awake." {
		t.Error("voice catalogue missing awake")
	}
	for moment, options := range phrases {
		for _, o := range options {
			if strings.Contains(o, "!") || o != strings.ToLower(o) {
				t.Errorf("%s: %q breaks the voice rules (lowercase, no exclamation marks)", moment, o)
			}
		}
	}
}
