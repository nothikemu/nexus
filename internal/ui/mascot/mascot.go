// Package mascot renders Nex, the Nexus core — the small, round data-being
// that lives in your terminal with you.
//
// Nex is a soft lavender-to-aqua blob with big eyes, rosy cheeks, stubby
// feet and a spark for an antenna. Its little arms hold two network nodes:
// it is, literally, a nexus — the thing in the middle everything connects to.
//
//	       ✦
//	    ▗▄▄▄▄▄▖
//	   ▟ ◕ ω ◕ ▙
//	●──▜▒     ▒▛──●
//	    ▝▀▘ ▝▀▘
//
// In colour the body is drawn with background-coloured cells and quadrant
// blocks so it reads as a solid, glowing shape. Without colour it becomes a
// line-art sprite with the same geometry. Plain mode omits it entirely.
package mascot

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nothikemu/nexus/internal/ui"
)

// Name is what the core is called.
const Name = "nex"

// State is a mood.
type State int

// Nex's moods.
const (
	Idle State = iota
	Thinking
	Working
	Success
	Warning
	Error
	Sleeping
	Connecting
	Celebrating
	Curious
	Waving
	Love
)

// States lists every mood in display order.
var States = []State{Idle, Waving, Thinking, Working, Connecting, Success, Celebrating, Love, Curious, Warning, Error, Sleeping}

var stateNames = map[State]string{
	Idle: "idle", Thinking: "thinking", Working: "working", Success: "success",
	Warning: "warning", Error: "error", Sleeping: "sleeping", Connecting: "connecting",
	Celebrating: "celebrating", Curious: "curious", Waving: "waving", Love: "love",
}

var stateCaptions = map[State]string{
	Idle:        "nex is here. what are we building?",
	Waving:      "hi. i'm nex.",
	Thinking:    "hmm, let me think…",
	Working:     "on it…",
	Success:     "nice. that worked.",
	Warning:     "psst — this needs a look.",
	Error:       "oops. something broke.",
	Sleeping:    "zzz… the database is napping.",
	Connecting:  "reaching out…",
	Celebrating: "everything is connected.",
	Curious:     "ooh, i noticed something.",
	Love:        "aw. thank you.",
}

func (s State) String() string { return stateNames[s] }

// Caption is the line Nex says in this mood.
func (s State) Caption() string { return stateCaptions[s] }

// Parse resolves a mood by name.
func Parse(name string) (State, bool) {
	for s, n := range stateNames {
		if strings.EqualFold(n, name) {
			return s, true
		}
	}
	return Idle, false
}

// Geometry of the portrait.
const (
	Width  = 15
	Height = 5

	bodyL  = 3  // left edge column of the body
	bodyR  = 11 // right edge column of the body
	eyeL   = 5
	eyeR   = 9
	mouthX = 7
	sparkX = 7
	faceY  = 2 // eyes and mouth
	armY   = 3 // cheeks and level arms
)

type pose int

const (
	poseLevel pose = iota
	poseRaised
	poseLowered
	poseBroken
	poseWave // left arm level, right arm up
)

// expression is the full description of one animation frame.
type expression struct {
	eyes   [2]rune
	look   int // -1 glance left, 0 centre, +1 glance right
	mouth  rune
	blush  bool
	spark  rune
	pose   pose
	nodes  [2]rune
	tint   string    // node/spark colour override (hex), "" = brand
	dim    bool      // sleeping body colours
	extras []overlay // sparkles, z's, packets, hearts
}

type overlay struct {
	x, y int
	ch   rune
	hex  string
}

// frame computes the expression for mood s at animation frame n.
func frame(t *ui.Theme, s State, n int) expression {
	p := t.Palette
	e := expression{
		eyes:  [2]rune{'◕', '◕'},
		mouth: 'ω',
		blush: true,
		spark: '✦',
		pose:  poseLevel,
		nodes: [2]rune{'●', '●'},
	}
	twinkle := func(period int) rune {
		if (n/period)%2 == 1 {
			return '✧'
		}
		return '✦'
	}
	switch s {
	case Idle:
		e.spark = twinkle(6)
		// A little life: glance around now and then, and blink.
		switch n % 40 {
		case 12, 13, 14:
			e.look = -1
		case 26, 27, 28:
			e.look = 1
		case 39:
			e.eyes = [2]rune{'‿', '‿'}
		}
	case Waving:
		e.eyes = [2]rune{'◠', '◠'}
		e.mouth = 'ᴗ'
		e.spark = twinkle(2)
		if (n/2)%2 == 0 {
			e.pose = poseWave
		}
	case Thinking:
		e.eyes = [2]rune{'◔', '◔'}
		e.look = 1
		e.mouth = '~'
		e.spark = []rune{'·', '∙', '✧', '✦', '✧', '∙'}[n%6]
		for i := 0; i < n%4 && i < 3; i++ {
			e.extras = append(e.extras, overlay{x: sparkX + 2 + i, y: 0, ch: '·', hex: p.Muted})
		}
	case Working:
		e.eyes = [2]rune{'•', '•'}
		e.mouth = 'ᴗ'
		e.spark = twinkle(3)
		pos := n % 3
		if pos < 2 {
			e.extras = append(e.extras,
				overlay{x: 1 + pos, y: armY, ch: '•', hex: p.Spark},
				overlay{x: 13 - pos, y: armY, ch: '•', hex: p.Spark})
		}
	case Connecting:
		e.nodes = [2]rune{'○', '○'}
		e.mouth = 'o'
		switch n % 4 {
		case 0, 1:
			pos := n % 4
			e.extras = append(e.extras,
				overlay{x: 2 - pos, y: armY, ch: '•', hex: p.Spark},
				overlay{x: 12 + pos, y: armY, ch: '•', hex: p.Spark})
		case 2:
			e.nodes = [2]rune{'◉', '◉'}
		case 3:
			e.nodes = [2]rune{'●', '●'}
		}
		e.spark = twinkle(2)
	case Success:
		e.eyes = [2]rune{'◠', '◠'}
		e.mouth = 'ᴗ'
		e.tint = p.Success
	case Celebrating:
		e.eyes = [2]rune{'✧', '✧'}
		e.mouth = 'ᴗ'
		e.pose = poseRaised
		sparkles := [][]overlay{
			{{x: 4, y: 0, ch: '✧', hex: p.Spark}, {x: 10, y: 0, ch: '⋆', hex: p.Secondary}},
			{{x: 4, y: 0, ch: '⋆', hex: p.Primary}, {x: 10, y: 0, ch: '✧', hex: p.Spark}},
		}
		e.extras = append(e.extras, sparkles[(n/3)%2]...)
		e.spark = twinkle(3)
	case Love:
		e.eyes = [2]rune{'♥', '♥'}
		e.mouth = 'ᴗ'
		e.spark = ' '
		hearts := [][]overlay{
			{{x: 9, y: 0, ch: '♥', hex: p.Blush}},
			{{x: 10, y: 0, ch: '♡', hex: p.Blush}, {x: 5, y: 0, ch: '♥', hex: p.Blush}},
			{{x: 4, y: 0, ch: '♡', hex: p.Blush}},
		}
		e.extras = append(e.extras, hearts[(n/3)%3]...)
	case Curious:
		e.eyes = [2]rune{'◕', '◉'}
		e.look = 1
		e.mouth = 'o'
		e.spark = '?'
		e.tint = p.Secondary
	case Warning:
		e.eyes = [2]rune{'◉', '◉'}
		e.mouth = '~'
		e.blush = false
		e.spark = '!'
		e.tint = p.Warning
	case Error:
		e.eyes = [2]rune{'×', '×'}
		e.mouth = '^'
		e.blush = false
		e.spark = ' '
		e.pose = poseBroken
		e.nodes = [2]rune{'○', '○'}
		e.tint = p.Error
	case Sleeping:
		e.eyes = [2]rune{'‿', '‿'}
		e.mouth = '.'
		e.blush = false
		e.spark = ' '
		e.pose = poseLowered
		e.nodes = [2]rune{'○', '○'}
		e.dim = true
		zs := [][]overlay{
			{{x: 10, y: 0, ch: 'z', hex: p.Muted}},
			{{x: 10, y: 0, ch: 'z', hex: p.Muted}, {x: 12, y: 0, ch: 'Z', hex: p.Faint}},
			{{x: 12, y: 0, ch: 'Z', hex: p.Faint}},
			{},
		}
		e.extras = append(e.extras, zs[(n/4)%4]...)
	}
	return e
}

// Loop is the number of frames in a mood's natural animation cycle.
func Loop(s State) int {
	switch s {
	case Idle:
		return 40
	case Thinking:
		return 12
	case Working:
		return 6
	case Connecting:
		return 8
	case Celebrating:
		return 6
	case Waving:
		return 8
	case Love:
		return 9
	case Sleeping:
		return 16
	default:
		return 1
	}
}

// cell is one terminal cell of the sprite.
type cell struct {
	ch   rune
	fg   string
	bg   string
	bold bool
}

type grid [Height][Width]cell

func blank() grid {
	var g grid
	for y := range g {
		for x := range g[y] {
			g[y][x] = cell{ch: ' '}
		}
	}
	return g
}

// Portrait renders Nex (five lines) in mood s at frame n.
// Returns "" in plain mode.
func Portrait(t *ui.Theme, s State, n int) string {
	if t.Mode.Plain || !t.Mode.Unicode {
		return ""
	}
	e := frame(t, s, n)
	g := blank()
	if t.Mode.Color {
		drawSolid(t, &g, e)
	} else {
		drawLineArt(&g, e)
	}
	drawArms(t, &g, e)
	for _, o := range e.extras {
		if o.y >= 0 && o.y < Height && o.x >= 0 && o.x < Width {
			bg := g[o.y][o.x].bg
			g[o.y][o.x] = cell{ch: o.ch, fg: o.hex, bg: bg}
		}
	}
	if e.spark != ' ' {
		hex := t.Palette.Spark
		if e.tint != "" && (s == Warning || s == Curious) {
			hex = e.tint
		}
		g[0][sparkX] = cell{ch: e.spark, fg: hex, bold: true}
	}
	return render(t, g)
}

func bodyColor(t *ui.Theme, x int, dim bool) string {
	p := t.Palette
	f := float64(x-bodyL) / float64(bodyR-bodyL)
	if dim {
		return ui.Blend(p.CoreDim, ui.Blend(p.CoreDim, p.CoreTo, 0.25), f)
	}
	return ui.Blend(p.CoreFrom, p.CoreTo, f)
}

// face places eyes and mouth, shifted by the glance direction.
func face(e expression) (l, r, m int) {
	return eyeL + e.look, eyeR + e.look, mouthX + e.look
}

func drawSolid(t *ui.Theme, g *grid, e expression) {
	p := t.Palette
	ink := p.Eye
	if e.dim {
		ink = ui.Blend(p.Eye, p.CoreDim, 0.5)
	}
	// A soft round body: narrow top, bevelled flanks, two stubby feet.
	for x := bodyL; x <= bodyR; x++ {
		col := bodyColor(t, x, e.dim)
		switch x {
		case bodyL:
			g[faceY][x] = cell{ch: '▟', fg: col}
			g[armY][x] = cell{ch: '▜', fg: col}
		case bodyR:
			g[faceY][x] = cell{ch: '▙', fg: col}
			g[armY][x] = cell{ch: '▛', fg: col}
		default:
			top := '▄'
			switch x {
			case bodyL + 1:
				top = '▗'
			case bodyR - 1:
				top = '▖'
			}
			g[1][x] = cell{ch: top, fg: col}
			g[faceY][x] = cell{ch: ' ', bg: col}
			g[armY][x] = cell{ch: ' ', bg: col}
		}
	}
	// Feet: ▝▀▘ ▝▀▘
	feet := map[int]rune{4: '▝', 5: '▀', 6: '▘', 8: '▝', 9: '▀', 10: '▘'}
	for x, ch := range feet {
		g[4][x] = cell{ch: ch, fg: bodyColor(t, x, e.dim)}
	}
	l, r, m := face(e)
	g[faceY][l] = cell{ch: e.eyes[0], fg: ink, bg: bodyColor(t, l, e.dim), bold: true}
	g[faceY][r] = cell{ch: e.eyes[1], fg: ink, bg: bodyColor(t, r, e.dim), bold: true}
	if e.mouth != ' ' {
		g[faceY][m] = cell{ch: e.mouth, fg: ink, bg: bodyColor(t, m, e.dim), bold: true}
	}
	if e.blush && !e.dim {
		for _, x := range []int{bodyL + 1, bodyR - 1} {
			g[armY][x] = cell{ch: '▃', fg: ui.Blend(bodyColor(t, x, false), p.Blush, 0.75), bg: bodyColor(t, x, false)}
		}
	}
}

func drawLineArt(g *grid, e expression) {
	g[1][bodyL], g[1][bodyR] = cell{ch: '╭'}, cell{ch: '╮'}
	for x := bodyL + 1; x < bodyR; x++ {
		g[1][x] = cell{ch: '─'}
	}
	for _, y := range []int{faceY, armY} {
		g[y][bodyL], g[y][bodyR] = cell{ch: '│'}, cell{ch: '│'}
	}
	if e.pose == poseLevel || e.pose == poseWave {
		g[armY][bodyL] = cell{ch: '┤'}
	}
	if e.pose == poseLevel {
		g[armY][bodyR] = cell{ch: '├'}
	}
	for x, ch := range map[int]rune{3: '╰', 4: '┬', 5: '─', 6: '╯', 8: '╰', 9: '─', 10: '┬', 11: '╯'} {
		g[4][x] = cell{ch: ch}
	}
	g[4][7] = cell{ch: ' '}
	l, r, m := face(e)
	g[faceY][l] = cell{ch: e.eyes[0]}
	g[faceY][r] = cell{ch: e.eyes[1]}
	if e.mouth != ' ' {
		g[faceY][m] = cell{ch: e.mouth}
	}
	if e.blush {
		g[armY][bodyL+1], g[armY][bodyR-1] = cell{ch: '·'}, cell{ch: '·'}
	}
}

func drawArms(t *ui.Theme, g *grid, e expression) {
	p := t.Palette
	linkL := ui.Blend(p.CoreFrom, p.Faint, 0.45)
	linkR := ui.Blend(p.CoreTo, p.Faint, 0.45)
	nodeL, nodeR := p.CoreFrom, p.CoreTo
	if e.tint != "" {
		nodeL, nodeR = e.tint, e.tint
	}
	if e.dim {
		linkL, linkR, nodeL, nodeR = p.Faint, p.Faint, p.Muted, p.Muted
	}
	if !t.Mode.Color {
		linkL, linkR, nodeL, nodeR = "", "", "", ""
	}
	levelL := func() {
		g[armY][0] = cell{ch: e.nodes[0], fg: nodeL, bold: true}
		g[armY][1] = cell{ch: '─', fg: linkL}
		g[armY][2] = cell{ch: '─', fg: linkL}
	}
	levelR := func() {
		g[armY][12] = cell{ch: '─', fg: linkR}
		g[armY][13] = cell{ch: '─', fg: linkR}
		g[armY][14] = cell{ch: e.nodes[1], fg: nodeR, bold: true}
	}
	raisedR := func() {
		g[0][13] = cell{ch: e.nodes[1], fg: nodeR, bold: true}
		g[1][12] = cell{ch: '╱', fg: linkR}
	}
	switch e.pose {
	case poseLevel:
		levelL()
		levelR()
	case poseWave:
		levelL()
		raisedR()
	case poseRaised:
		g[0][1] = cell{ch: e.nodes[0], fg: nodeL, bold: true}
		g[1][2] = cell{ch: '╲', fg: linkL}
		raisedR()
	case poseLowered:
		g[4][1] = cell{ch: e.nodes[0], fg: nodeL}
		g[armY][2] = cell{ch: '╱', fg: linkL}
		g[4][13] = cell{ch: e.nodes[1], fg: nodeR}
		g[armY][12] = cell{ch: '╲', fg: linkR}
	case poseBroken:
		g[armY][0] = cell{ch: e.nodes[0], fg: nodeL}
		g[armY][1] = cell{ch: '╌', fg: linkL}
		g[armY][13] = cell{ch: '╌', fg: linkR}
		g[armY][14] = cell{ch: e.nodes[1], fg: nodeR}
	}
}

func render(t *ui.Theme, g grid) string {
	lines := make([]string, Height)
	for y := 0; y < Height; y++ {
		var b strings.Builder
		for x := 0; x < Width; x++ {
			c := g[y][x]
			if !t.Mode.Color || (c.fg == "" && c.bg == "") {
				b.WriteRune(c.ch)
				continue
			}
			st := t.R.NewStyle()
			if c.fg != "" {
				st = st.Foreground(lipgloss.Color(c.fg))
			}
			if c.bg != "" {
				st = st.Background(lipgloss.Color(c.bg))
			}
			if c.bold {
				st = st.Bold(true)
			}
			b.WriteString(st.Render(string(c.ch)))
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

// Face renders the single-line pill face used in headers and prompts:
// a 7-cell rounded body with two eyes. Without colour it is "(◉ ◉)";
// in plain mode it returns "".
func Face(t *ui.Theme, s State, n int) string {
	if t.Mode.Plain || !t.Mode.Unicode {
		return ""
	}
	e := frame(t, s, n)
	if !t.Mode.Color {
		return "(" + string(e.eyes[0]) + string(e.mouth) + string(e.eyes[1]) + ")"
	}
	p := t.Palette
	from, to := p.CoreFrom, p.CoreTo
	eye := p.Eye
	if e.dim {
		from, to, eye = p.CoreDim, ui.Blend(p.CoreDim, p.CoreTo, 0.25), ui.Blend(p.Eye, p.CoreDim, 0.5)
	}
	col := func(i int) string { return ui.Blend(from, to, float64(i)/6) }
	var b strings.Builder
	b.WriteString(t.Style(col(0)).Render("▐"))
	inner := []rune{' ', e.eyes[0], e.mouth, e.eyes[1], ' '}
	for i, r := range inner {
		st := t.R.NewStyle().Background(lipgloss.Color(col(i + 1)))
		if r != ' ' {
			st = st.Foreground(lipgloss.Color(eye)).Bold(true)
		}
		b.WriteString(st.Render(string(r)))
	}
	b.WriteString(t.Style(col(6)).Render("▌"))
	return b.String()
}

// Glyph is the one-cell mark of a state, for inline use.
func Glyph(t *ui.Theme, s State) string {
	g := t.Glyphs
	switch s {
	case Success, Celebrating:
		return t.Success.Bold(true).Render(g.Insight)
	case Love:
		if t.Mode.Unicode {
			return t.Style(t.Palette.Blush).Bold(true).Render("♥")
		}
		return "<3"
	case Warning:
		return t.Warning.Bold(true).Render(g.Warning)
	case Error:
		return t.Error.Bold(true).Render(g.Error)
	case Sleeping:
		return t.Muted.Render(g.DotOff)
	case Curious:
		return t.Secondary.Bold(true).Render(g.Insight)
	default:
		return t.Primary.Bold(true).Render(g.Nexus)
	}
}

// Hero composes the portrait above the wordmark and a caption, centered in
// width cells — used by `nexus init`, `nexus mascot` and the dashboard.
func Hero(t *ui.Theme, s State, n int, caption string, width int) string {
	var lines []string
	if p := Portrait(t, s, n); p != "" {
		for _, l := range strings.Split(p, "\n") {
			lines = append(lines, ui.Center(l, width))
		}
		lines = append(lines, "")
	}
	lines = append(lines, ui.Center(t.Wordmark(), width))
	if caption != "" {
		lines = append(lines, "", ui.Center(t.Muted.Render(caption), width))
	}
	return strings.Join(lines, "\n")
}
