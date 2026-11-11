package ui

// Keymap: every binding is a non-printing key so a Chinese IME never
// swallows it (FeedScope's rule). Printable keys get a hint instead.
type binding struct {
	key  string // literal key as shown in help, e.g. "Tab / Shift+Tab"
	desc string // i18n key under help.keys.*
}

// helpBindings are the bindings wired today; milestones append
// without touching the help renderer.
var helpBindings = []binding{
	{"Tab / Shift+Tab", "switch_source"},
	{"← → ↑ ↓", "region_nav"},
	{"Enter", "open"},
	{"Esc", "back"},
	{"F1", "help"},
	{"F2", "sources"},
	{"F3", "favorites"},
	{"F4", "unread_only"},
	{"F5", "refresh"},
	{"Ctrl+O", "browser"},
	{"Ctrl+Y", "copy_link"},
	{"Ctrl+T", "theme"},
	{"Ctrl+Q", "quit"},
}
