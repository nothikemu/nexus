package ui

// Glyphs is the symbol set for a mode. Unicode glyphs are chosen to be
// single-cell in every mainstream terminal font; plain mode uses ASCII so
// screen readers and log collectors get clean text.
type Glyphs struct {
	Nexus    string // ✦ nexus speaking
	Insight  string // ◈ success / insight
	Warning  string // ▲
	Error    string // ✕
	Info     string // ◇
	Arrow    string // → hints
	Check    string // ✓ step done
	Cross    string // ✕ step failed
	Skip     string // – step skipped
	Pending  string // ○ not yet
	Dot      string // ● live state
	DotOff   string // ○ stopped state
	Bullet   string // •
	Sep      string // · inline separator
	Ellipsis string // …
	Prompt   string // ❯ input prompt
	Plus     string // + added
	Minus    string // - removed
	Tilde    string // ~ changed
	TreeMid  string // ├─
	TreeEnd  string // └─
	TreePipe string // │
	Rule     string // ─ horizontal rule cell
	Key      string // ⚿ primary key marker
	Link     string // ↗ foreign key marker
	Lock     string // ⛨ row-level security marker
	Up       string // ↑ sort ascending
	Down     string // ↓ sort descending
}

var unicodeGlyphs = Glyphs{
	Nexus: "✦", Insight: "◈", Warning: "▲", Error: "✕", Info: "◇", Arrow: "→",
	Check: "✓", Cross: "✕", Skip: "–", Pending: "○", Dot: "●", DotOff: "○",
	Bullet: "•", Sep: "·", Ellipsis: "…", Prompt: "❯",
	Plus: "+", Minus: "−", Tilde: "~",
	TreeMid: "├─", TreeEnd: "└─", TreePipe: "│", Rule: "─",
	Key: "◆", Link: "↗", Lock: "◍", Up: "↑", Down: "↓",
}

var asciiGlyphs = Glyphs{
	Nexus: "*", Insight: "+", Warning: "!", Error: "x", Info: "i", Arrow: "->",
	Check: "ok", Cross: "x", Skip: "-", Pending: "o", Dot: "*", DotOff: "o",
	Bullet: "-", Sep: "|", Ellipsis: "...", Prompt: ">",
	Plus: "+", Minus: "-", Tilde: "~",
	TreeMid: "|-", TreeEnd: "`-", TreePipe: "|", Rule: "-",
	Key: "PK", Link: "FK", Lock: "RLS", Up: "^", Down: "v",
}

func glyphsFor(m Mode) Glyphs {
	if m.Unicode && !m.Plain {
		return unicodeGlyphs
	}
	return asciiGlyphs
}
