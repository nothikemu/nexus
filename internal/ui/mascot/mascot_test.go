package mascot

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nothikemu/nexus/internal/ui"
)

func themes() map[string]*ui.Theme {
	m := ui.RichMode(100)
	nc := m
	nc.Color = false
	return map[string]*ui.Theme{"rich": ui.NewTheme(m, &bytes.Buffer{}), "nocolor": ui.NewTheme(nc, &bytes.Buffer{})}
}

// Every frame of every state must be exactly Width×Height cells, so
// animations redraw in place without jitter.
func TestPortraitGeometry(t *testing.T) {
	for name, th := range themes() {
		for _, s := range States {
			for n := 0; n < Loop(s)*2; n++ {
				p := Portrait(th, s, n)
				lines := strings.Split(p, "\n")
				if len(lines) != Height {
					t.Fatalf("%s/%s frame %d: %d lines", name, s, n, len(lines))
				}
				for i, l := range lines {
					if w := ui.Width(l); w != Width {
						t.Fatalf("%s/%s frame %d line %d: width %d: %q", name, s, n, i, w, l)
					}
				}
			}
		}
	}
}

func TestFaceAndPlain(t *testing.T) {
	for name, th := range themes() {
		f := Face(th, Idle, 0)
		want := 7
		if name == "nocolor" {
			want = 5
		}
		if ui.Width(f) != want {
			t.Errorf("%s face width = %d (%q)", name, ui.Width(f), f)
		}
	}
	plain := ui.NewTheme(ui.PlainMode(), &bytes.Buffer{})
	if Portrait(plain, Idle, 0) != "" || Face(plain, Idle, 0) != "" {
		t.Error("plain mode must not draw the mascot")
	}
	if Glyph(plain, Error) != "x" {
		t.Errorf("plain glyph = %q", Glyph(plain, Error))
	}
}

func TestExpressionsAreDistinct(t *testing.T) {
	th := themes()["nocolor"]
	seen := map[string]State{}
	for _, s := range States {
		p := Portrait(th, s, 0)
		if other, dup := seen[p]; dup {
			t.Errorf("%s looks identical to %s", s, other)
		}
		seen[p] = s
	}
}

func TestParse(t *testing.T) {
	for _, s := range States {
		got, ok := Parse(strings.ToUpper(s.String()))
		if !ok || got != s {
			t.Errorf("Parse(%s) = %v, %v", s, got, ok)
		}
		if s.Caption() == "" {
			t.Errorf("%s has no caption", s)
		}
	}
	if _, ok := Parse("grumpy"); ok {
		t.Error("unknown state parsed")
	}
}
