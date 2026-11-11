// Package theme holds readscope's visual identity: two built-in themes
// (dark default, light), editor-inspired palettes, applied via lipgloss.
// A theme is a plain struct of styles; future TOML themes map onto the
// same fields.
package theme

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"

	rendr "github.com/smy-101/readscope/internal/render"
)

// Default is the fallback theme name.
const Default = "dark"

// Theme is a resolved set of styles. Grouped by UI region.
type Theme struct {
	Name string

	// Top source bar: container + tabs.
	SourceBar      lipgloss.Style
	SourceActive   lipgloss.Style
	SourceInactive lipgloss.Style

	// Left sidebar: feed rows.
	Sidebar          lipgloss.Style
	SidebarItem      lipgloss.Style
	SidebarSelected  lipgloss.Style
	SidebarBadge     lipgloss.Style
	SidebarBadgeZero lipgloss.Style

	// Item list: leading unread dot, titles, meta column.
	List         lipgloss.Style
	UnreadDot    lipgloss.Style
	Title        lipgloss.Style
	TitleRead    lipgloss.Style
	Meta         lipgloss.Style
	ListSelected lipgloss.Style

	// Status line.
	StatusBar    lipgloss.Style
	StatusNormal lipgloss.Style
	StatusAccent lipgloss.Style
	StatusHint   lipgloss.Style

	// Help overlay.
	HelpOverlay lipgloss.Style
	HelpTitle   lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style

	// Reader (EPUB full screen) — colors wired in M4, reserved now.
	ReaderBody   lipgloss.Style
	ReaderTOC    lipgloss.Style
	ReaderActive lipgloss.Style

	// Syntax / inline-semantic colors (chroma categories, links, inline code).
	Code    lipgloss.Style
	Kw      lipgloss.Style
	Str     lipgloss.Style
	Num     lipgloss.Style
	Com     lipgloss.Style
	Link    lipgloss.Style
	Marker  lipgloss.Style
	Underln lipgloss.Style

	// Semantics.
	Accent  lipgloss.Style
	Dim     lipgloss.Style
	Error   lipgloss.Style
	Success lipgloss.Style
	Warning lipgloss.Style

	// Body is plain reading text.
	Body lipgloss.Style
}

type palette struct {
	barBg, barFg            string // source/status bar
	accent, accentBg        string
	fg, dim                 string
	active, inactive        string // source tabs
	selBg                   string // selection backgrounds
	error, success, warning string
	helpBg                  string
	code, kw, str, num, com string // syntax
	link                    string
}

var builtins = map[string]palette{
	// editor-dark: VS Code Dark+ oriented, transparent body background.
	"dark": {
		barBg: "#1e1e1e", barFg: "#cccccc",
		accent: "#569cd6", accentBg: "#094771",
		fg: "#d4d4d4", dim: "#6a6a6a",
		active: "#ffffff", inactive: "#858585",
		selBg: "#094771",
		error: "#f14c4c", success: "#89d185", warning: "#cca700",
		helpBg: "#252526",
		code:   "#d4d4d4", kw: "#569cd6", str: "#ce9178", num: "#b5cea8", com: "#6a9955",
		link: "#4ec9b0",
	},
	// editor-light: VS Code Light+ oriented.
	"light": {
		barBg: "#f3f3f3", barFg: "#333333",
		accent: "#0451a5", accentBg: "#cce3f5",
		fg: "#1f1f1f", dim: "#8a8a8a",
		active: "#000000", inactive: "#6f6f6f",
		selBg: "#cce3f5",
		error: "#e51400", success: "#388a34", warning: "#bf8803",
		helpBg: "#ffffff",
		code:   "#1f1f1f", kw: "#0000ff", str: "#a31515", num: "#098658", com: "#008000",
		link: "#0451a5",
	},
}

// Load resolves a theme by name; unknown names fall back to Default.
func Load(name string) (*Theme, error) {
	p, ok := builtins[name]
	if !ok {
		p = builtins[Default]
		name = Default
	}
	return build(name, p), nil
}

// Available lists built-in theme names, sorted.
func Available() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (t *Theme) String() string { return fmt.Sprintf("theme(%s)", t.Name) }

func build(name string, p palette) *Theme {
	bar := lipgloss.NewStyle().Background(lipgloss.Color(p.barBg)).Foreground(lipgloss.Color(p.barFg))
	return &Theme{
		Name: name,

		SourceBar:      bar.Padding(0, 1),
		SourceActive:   lipgloss.NewStyle().Background(lipgloss.Color(p.accentBg)).Foreground(lipgloss.Color(p.active)).Bold(true).Padding(0, 1),
		SourceInactive: lipgloss.NewStyle().Background(lipgloss.Color(p.barBg)).Foreground(lipgloss.Color(p.inactive)).Padding(0, 1),

		Sidebar:          lipgloss.NewStyle().Border(lipgloss.Border{Left: "│"}, false, false, false, true).BorderForeground(lipgloss.Color(p.dim)).PaddingLeft(1),
		SidebarItem:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)),
		SidebarSelected:  lipgloss.NewStyle().Background(lipgloss.Color(p.selBg)).Foreground(lipgloss.Color(p.active)).Bold(true),
		SidebarBadge:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)),
		SidebarBadgeZero: lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),

		List:         lipgloss.NewStyle().PaddingLeft(1),
		UnreadDot:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)),
		Title:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)),
		TitleRead:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),
		Meta:         lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),
		ListSelected: lipgloss.NewStyle().Background(lipgloss.Color(p.selBg)).Foreground(lipgloss.Color(p.active)),

		StatusBar:    bar.Padding(0, 1),
		StatusNormal: lipgloss.NewStyle().Foreground(lipgloss.Color(p.barFg)),
		StatusAccent: lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true),
		StatusHint:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),

		HelpOverlay: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(p.accent)).Background(lipgloss.Color(p.helpBg)).Padding(1, 2),
		HelpTitle:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true),
		HelpKey:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.active)).Bold(true),
		HelpDesc:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)),

		ReaderBody:   lipgloss.NewStyle(),
		ReaderTOC:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),
		ReaderActive: lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true),

		Code:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.code)),
		Kw:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.kw)),
		Str:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.str)),
		Num:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.num)),
		Com:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.com)),
		Link:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.link)).Underline(true),
		Marker:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)),
		Underln: lipgloss.NewStyle().Underline(true),

		Accent:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.error)),
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color(p.success)),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color(p.warning)),

		Body: lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)),
	}
}

// StyleFor maps render span-style bits to a terminal style — the bridge
// used by render.ToANSI (and later the TUI viewer). Syntax categories win
// over plain code; emphasis bits apply on top.
//
// Note: lipgloss v1 renders Underline per-rune (termenv behavior), so
// links cost ~2× escape bytes. Correct visually; revisit only if TUI
// scrolling performance ever cares.
func (t *Theme) StyleFor(bits rendr.SpanStyle) lipgloss.Style {
	var s lipgloss.Style
	has := func(b rendr.SpanStyle) bool { return bits&b != 0 }
	switch {
	case has(rendr.SpanKw):
		s = t.Kw
	case has(rendr.SpanStr):
		s = t.Str
	case has(rendr.SpanNum):
		s = t.Num
	case has(rendr.SpanCom):
		s = t.Com
	case has(rendr.SpanCode):
		s = t.Code
	case has(rendr.SpanLink):
		s = t.Link
	case has(rendr.SpanMarker):
		s = t.Marker
	case has(rendr.SpanQuote), has(rendr.SpanDim):
		s = t.Dim
	default:
		s = t.Body
	}
	if has(rendr.SpanBold) {
		s = s.Bold(true)
	}
	if has(rendr.SpanItalic) {
		s = s.Italic(true)
	}
	if has(rendr.SpanUnderline) {
		s = s.Underline(true)
	}
	return s
}
