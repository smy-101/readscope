// Package ui is the Bubble Tea front end: the FeedScope-style shell
// (source bar / sidebar / list / status bar) with the EPUB reader joining
// in M4. M0 renders the empty shell with region navigation, help overlay
// and live theme switching.
package ui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/theme"
)

// sourceIDs doubles as the source bar order (plan decision #11).
var sourceIDs = []string{"books", "rss", "hn"}

type region int

const (
	regionSourceBar region = iota
	regionSidebar
	regionList
)

const sidebarWidth = 24

// Model is the root Bubble Tea model.
type Model struct {
	cfg *core.Config
	cat *i18n.Catalog
	th  *theme.Theme

	width, height int

	active     int // index into sourceIDs
	focus      region
	sidebarSel int

	help   bool
	status string // transient hint in the status bar
}

// New builds the root model from loaded configuration.
func New(cfg *core.Config, cat *i18n.Catalog, th *theme.Theme) Model {
	return Model{cfg: cfg, cat: cat, th: th, focus: regionList}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+q":
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.active = (m.active + 1) % len(sourceIDs)
			m.focus, m.sidebarSel = regionSidebar, 0
			return m, nil
		case "shift+tab":
			m.active = (m.active - 1 + len(sourceIDs)) % len(sourceIDs)
			m.focus, m.sidebarSel = regionSidebar, 0
			return m, nil

		case "up":
			if m.focus == regionSourceBar { // switch site in place
				m.active = (m.active - 1 + len(sourceIDs)) % len(sourceIDs)
				return m, nil
			}
			if m.focus == regionSidebar && m.sidebarSel > 0 {
				m.sidebarSel--
			}
			return m, nil
		case "down":
			if m.focus == regionSourceBar {
				m.active = (m.active + 1) % len(sourceIDs)
				return m, nil
			}
			if m.focus == regionSidebar { // bounded by feed count
				if m.sidebarSel < len(m.sidebarRows())-1 {
					m.sidebarSel++
				}
			}
			return m, nil

		case "left":
			if m.help {
				m.help = false
			} else if m.focus == regionList {
				m.focus = regionSidebar
			} else if m.focus == regionSidebar {
				m.focus = regionSourceBar
			}
			return m, nil
		case "right":
			if m.focus == regionSourceBar {
				m.focus = regionSidebar
			} else if m.focus == regionSidebar {
				m.focus = regionList
			}
			return m, nil

		case "enter":
			if m.focus == regionSourceBar {
				m.focus = regionList
			} else if m.focus == regionSidebar {
				m.focus = regionList
			}
			return m, nil
		case "esc":
			if m.help {
				m.help = false
				return m, nil
			}
			m.focus = regionList
			return m, nil

		case "f1":
			m.help = !m.help
			return m, nil

		case "ctrl+t":
			m.cycleTheme()
			return m, nil

		case "f2", "f3", "f4", "f5":
			m.status = m.cat.Get("status.todo")
			return m, nil
		}

		// Printable keys while an IME might be active: hint, never act.
		if isPrintable(msg) {
			m.status = m.cat.Get("help.hint")
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return m.cat.Get("app.title")
	}
	if m.help {
		return m.helpView()
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.sidebarView(),
		m.listView(),
	)
	body = lipgloss.NewStyle().Height(m.bodyHeight()).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.sourceBarView(),
		body,
		m.statusBarView(),
	)
}

// --- regions -------------------------------------------------------------

func (m Model) sourceLabel(i int) string { return m.cat.Get("source." + sourceIDs[i]) }

func (m Model) sourceBarView() string {
	tabs := make([]string, len(sourceIDs))
	for i := range sourceIDs {
		style := m.th.SourceInactive
		if i == m.active {
			style = m.th.SourceActive
			if m.focus == regionSourceBar {
				style = style.Underline(true)
			}
		}
		tabs[i] = style.Render(" " + m.sourceLabel(i) + " ")
	}
	line := m.th.SourceBar.Render(strings.Join(tabs, ""))
	return lipgloss.NewStyle().Width(m.width).MaxHeight(1).Render(line)
}

// sidebarRows returns the sidebar entries for the active source. M0: one
// placeholder row per source; real feeds arrive with their milestones.
func (m Model) sidebarRows() []string {
	return []string{m.cat.Get("sidebar.no_feeds")}
}

func (m Model) sidebarView() string {
	var b strings.Builder
	b.WriteString(m.th.SidebarHeader.Render(m.cat.Get("sidebar.feeds")))
	b.WriteString("\n")
	for i, row := range m.sidebarRows() {
		if i == m.sidebarSel && m.focus == regionSidebar {
			b.WriteString(m.th.SidebarSelected.Render(" " + row))
		} else {
			b.WriteString(m.th.SidebarItem.Render(" " + row))
		}
		if i < len(m.sidebarRows())-1 {
			b.WriteString("\n")
		}
	}
	return m.th.Sidebar.
		Width(sidebarWidth).
		Height(m.bodyHeight()).
		Render(b.String())
}

func (m Model) listView() string {
	empty := m.th.Dim.Render(m.cat.Get("list.empty"))
	return m.th.List.
		Width(m.width - sidebarWidth - 2).
		Height(m.bodyHeight()).
		Render(empty)
}

func (m Model) statusBarView() string {
	left := m.th.StatusNormal.Render(m.sourceLabel(m.active))
	if m.status != "" {
		left += "  " + m.th.StatusAccent.Render(m.status)
	}
	right := m.th.StatusHint.Render(m.cat.Get("help.hint"))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return m.th.StatusBar.Width(m.width).MaxHeight(1).
		Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) helpView() string {
	rows := make([]string, 0, len(m0Bindings))
	for _, b := range m0Bindings {
		rows = append(rows, "  "+m.th.HelpKey.Render(pad(b.key, 18))+
			m.th.HelpDesc.Render(m.cat.Get("help.keys."+b.desc)))
	}
	box := m.th.HelpOverlay.Render(
		m.th.HelpTitle.Render(m.cat.Get("help.title")) + "\n\n" +
			strings.Join(rows, "\n") + "\n\n" +
			m.th.Dim.Render(m.cat.Get("help.close")),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- helpers -------------------------------------------------------------

func (m *Model) bodyHeight() int {
	h := m.height - 2 // source bar + status bar
	if h < 1 {
		h = 1
	}
	return h
}

// cycleTheme rotates through built-in themes, applying instantly and
// persisting the choice (FeedScope's live-switch model).
func (m *Model) cycleTheme() {
	names := theme.Available()
	next := names[0]
	for i, n := range names {
		if n == m.th.Name && i+1 < len(names) {
			next = names[i+1]
		}
	}
	th, err := theme.Load(next)
	if err != nil {
		return
	}
	m.th = th
	m.cfg.Theme = next
	if err := m.cfg.Save(); err != nil {
		m.status = m.cat.Get("errors.theme_save", err)
		return
	}
	m.status = m.cat.Get("status.theme", next)
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s + " "
	}
	return s + strings.Repeat(" ", w-len(s))
}

func isPrintable(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		return true
	}
	return false
}
