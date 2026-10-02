package ui

// Palette is the Nexus colour system. Each colour has exactly one job;
// colour establishes hierarchy and is never used for decoration alone.
type Palette struct {
	// Brand
	Primary   string // nexus violet — the voice of nexus, headings, the core
	Secondary string // signal cyan — values, links, the far end of the core gradient
	Spark     string // the mascot's spark and rare highlights

	// Text
	Strong string // emphasis
	Text   string // body copy
	Muted  string // metadata, labels
	Faint  string // rules, borders, placeholders

	// Semantic states
	Success string
	Warning string
	Error   string
	Info    string

	// Domains — used for small badges and dots, never for body text.
	Database  string
	Auth      string
	Storage   string
	Realtime  string
	Functions string
	Jobs      string
	AI        string

	// The core gradient, left to right.
	CoreFrom string
	CoreTo   string
	CoreDim  string // the core while sleeping
	Eye      string // eyes & mouth, drawn on the core
}

// DarkPalette is tuned for dark terminal backgrounds (the common case).
var DarkPalette = Palette{
	Primary:   "#A48BFF",
	Secondary: "#3CDCEB",
	Spark:     "#FFD6FF",

	Strong: "#F7F7FB",
	Text:   "#D9D9E3",
	Muted:  "#8A8AA0",
	Faint:  "#4A4A5E",

	Success: "#5BE49B",
	Warning: "#FFC145",
	Error:   "#FF6B7A",
	Info:    "#6CB4FF",

	Database:  "#5AC8FA",
	Auth:      "#C8A2FF",
	Storage:   "#FF9F5A",
	Realtime:  "#FF7EB6",
	Functions: "#4FE0B6",
	Jobs:      "#B5E655",
	AI:        "#F08CFF",

	CoreFrom: "#7B5CFF",
	CoreTo:   "#1FC8E3",
	CoreDim:  "#3B3566",
	Eye:      "#FFFFFF",
}

// LightPalette keeps the same identity with enough contrast on light backgrounds.
var LightPalette = Palette{
	Primary:   "#6A43F0",
	Secondary: "#0A8FA8",
	Spark:     "#B03CC8",

	Strong: "#14141C",
	Text:   "#2E2E3A",
	Muted:  "#6B6B80",
	Faint:  "#B9B9C8",

	Success: "#1E9E5A",
	Warning: "#B87800",
	Error:   "#D2344A",
	Info:    "#2A72D4",

	Database:  "#1A86C8",
	Auth:      "#8A4FE0",
	Storage:   "#D06A1A",
	Realtime:  "#D03C82",
	Functions: "#109A76",
	Jobs:      "#5E8F0E",
	AI:        "#B23ACB",

	CoreFrom: "#6A43F0",
	CoreTo:   "#0BA5C2",
	CoreDim:  "#A9A3CF",
	Eye:      "#FFFFFF",
}

// Domain identifies a Nexus subsystem for colour coding.
type Domain int

// Domains of the platform.
const (
	DomainDatabase Domain = iota
	DomainAuth
	DomainStorage
	DomainRealtime
	DomainFunctions
	DomainJobs
	DomainAI
)

func (p Palette) domain(d Domain) string {
	switch d {
	case DomainAuth:
		return p.Auth
	case DomainStorage:
		return p.Storage
	case DomainRealtime:
		return p.Realtime
	case DomainFunctions:
		return p.Functions
	case DomainJobs:
		return p.Jobs
	case DomainAI:
		return p.AI
	default:
		return p.Database
	}
}
