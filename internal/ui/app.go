// Package ui is the Bubble Tea front end: the FeedScope-style shell
// (source bar / sidebar / list / status bar) with the EPUB reader joining
// in M4. M0 renders the empty shell with region navigation, help overlay
// and live theme switching; M2 wires real RSS feeds into the sidebar and
// cached items into the list, cache first — the store is read on startup
// and after every refresh, and the network is only ever touched by F5.
package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/refresh"
	"github.com/smy-101/readscope/internal/source"
	"github.com/smy-101/readscope/internal/store"
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
	cfg  *core.Config
	cat  *i18n.Catalog
	th   *theme.Theme
	st   *store.Store
	srcs *core.Sources

	width, height int

	active     int // index into sourceIDs
	focus      region
	sidebarSel int
	listSel    int // cursor in m.items
	listTop    int // first visible list row (scroll window)

	// Cache-first list state, loaded via cmds from the store only.
	feeds      []core.Feed                // active source's sidebar entries
	counts     map[string]store.FeedCount // per feed key
	items      []store.ItemRow            // selected feed's items
	loadedFeed string                     // feed key of m.items ("" = none)

	refreshing bool   // F5 fetch in flight
	help       bool   // F1 overlay open
	status     string // transient hint in the status bar
}

// New builds the root model from loaded configuration, the item store
// and the subscription file. st must be open and migrated (main does
// that before the alt screen takes over).
func New(cfg *core.Config, cat *i18n.Catalog, th *theme.Theme, st *store.Store, srcs *core.Sources) Model {
	return Model{
		cfg: cfg, cat: cat, th: th, st: st, srcs: srcs,
		focus: regionList, counts: map[string]store.FeedCount{},
	}
}

// Init loads the sidebar and list from the SQLite cache — local reads
// only, so the list is on screen before (and without) any network.
func (m Model) Init() tea.Cmd { return m.loadFeedsCmd() }

// --- async messages ------------------------------------------------------

type feedsMsg struct {
	source string // source id the feeds belong to
	feeds  []core.Feed
	counts map[string]store.FeedCount
}

type itemsMsg struct {
	feed string
	rows []store.ItemRow
	err  error
}

type refreshDoneMsg struct {
	source  string // the source that was refreshed
	results []refresh.Result
	err     error
}

func (m Model) loadFeedsCmd() tea.Cmd {
	if m.st == nil {
		return nil
	}
	id := m.activeSourceID()
	srcs, st := m.srcs, m.st
	return func() tea.Msg {
		feeds := []core.Feed{}
		if src, err := source.New(id, srcs); err == nil {
			feeds = src.Feeds()
		}
		counts, err := st.FeedCounts(context.Background(), id)
		if err != nil {
			counts = map[string]store.FeedCount{}
		}
		return feedsMsg{source: id, feeds: feeds, counts: counts}
	}
}

func (m Model) loadItemsCmd() tea.Cmd {
	if m.st == nil {
		return nil
	}
	key := m.selectedFeedKey()
	if key == "" {
		return nil
	}
	id, st := m.activeSourceID(), m.st
	return func() tea.Msg {
		rows, err := st.FeedItems(context.Background(), id, key)
		return itemsMsg{feed: key, rows: rows, err: err}
	}
}

func (m Model) refreshCmd() tea.Cmd {
	if m.st == nil || m.srcs == nil {
		return nil
	}
	id, srcs, st := m.activeSourceID(), m.srcs, m.st
	return func() tea.Msg {
		results, err := refresh.Source(context.Background(), st, srcs, id)
		return refreshDoneMsg{source: id, results: results, err: err}
	}
}

// --- update --------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case feedsMsg:
		if msg.source != m.activeSourceID() {
			return m, nil // stale: the user moved on before the load landed
		}
		m.feeds, m.counts = msg.feeds, msg.counts
		m.sidebarClamp()
		// The cursor survives a same-feed reload (F5 landing); itemsMsg
		// decides reset vs clamp once it knows whether the feed changed.
		return m, m.loadItemsCmd()

	case itemsMsg:
		if msg.feed != m.selectedFeedKey() {
			return m, nil // stale in-flight load for another feed
		}
		sameFeed := msg.feed == m.loadedFeed
		m.items = msg.rows
		m.loadedFeed = msg.feed
		if sameFeed {
			m.clampListCursor()
		} else {
			m.listSel, m.listTop = 0, 0
		}
		if msg.err != nil {
			m.status = m.cat.Get("errors.db_query", msg.err)
		}
		return m, nil

	case refreshDoneMsg:
		m.refreshing = false
		m.status = m.refreshStatus(msg)
		return m, m.loadFeedsCmd() // counts + list pick up what just landed

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+q":
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.cycleSource(1)
			return m, m.loadFeedsCmd()
		case "shift+tab":
			m.cycleSource(-1)
			return m, m.loadFeedsCmd()

		case "up":
			if m.focus == regionSourceBar { // switch site in place
				m.cycleSource(-1)
				return m, m.loadFeedsCmd()
			}
			if m.focus == regionSidebar {
				if m.sidebarSel > 0 {
					m.sidebarSel--
					return m, m.loadItemsCmd()
				}
				return m, nil
			}
			m.moveListSel(-1)
			return m, nil
		case "down":
			if m.focus == regionSourceBar {
				m.cycleSource(1)
				return m, m.loadFeedsCmd()
			}
			if m.focus == regionSidebar {
				if m.sidebarSel < m.sidebarLen()-1 {
					m.sidebarSel++
					return m, m.loadItemsCmd()
				}
				return m, nil
			}
			m.moveListSel(1)
			return m, nil
		case "pgup":
			m.moveListSel(-m.bodyHeight())
			return m, nil
		case "pgdown":
			m.moveListSel(m.bodyHeight())
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
			if m.focus == regionSourceBar || m.focus == regionSidebar {
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

		case "f5":
			if m.refreshing {
				return m, nil
			}
			if _, err := source.New(m.activeSourceID(), m.srcs); err != nil {
				m.status = m.cat.Get("status.todo") // books/hn: no fetchable source yet
				return m, nil
			}
			m.refreshing = true
			return m, m.refreshCmd()

		case "f2", "f3", "f4":
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

func (m Model) activeSourceID() string { return sourceIDs[m.active] }

// cycleSource rotates the source bar by delta (wrapping), refocusing
// the sidebar and clearing the previous source's list state; feeds and
// counts refill when the loadFeeds cmd lands.
func (m *Model) cycleSource(delta int) {
	m.active = (m.active + delta + len(sourceIDs)) % len(sourceIDs)
	m.focus, m.sidebarSel = regionSidebar, 0
	m.resetSource()
}

// sidebarLen is the sidebar's row count (feeds, or the single
// placeholder row for sources without feeds yet).
func (m Model) sidebarLen() int {
	if len(m.feeds) == 0 {
		return 1
	}
	return len(m.feeds)
}

func (m Model) sidebarClamp() {
	if m.sidebarSel > m.sidebarLen()-1 {
		m.sidebarSel = 0
	}
}

// resetSource clears the previous source's list state; feeds and counts
// refill when the loadFeeds cmd lands.
func (m *Model) resetSource() {
	m.feeds = nil
	m.counts = map[string]store.FeedCount{}
	m.items = nil
	m.loadedFeed = ""
	m.listSel, m.listTop = 0, 0
}

func (m Model) selectedFeed() *core.Feed {
	if m.sidebarSel < len(m.feeds) {
		return &m.feeds[m.sidebarSel]
	}
	return nil
}

func (m Model) selectedFeedKey() string {
	if f := m.selectedFeed(); f != nil {
		return f.Key
	}
	return ""
}

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

func (m Model) sidebarView() string {
	var b strings.Builder
	b.WriteString(m.th.SidebarHeader.Render(m.cat.Get("sidebar.feeds")))
	b.WriteString("\n")
	if len(m.feeds) == 0 {
		b.WriteString(m.sidebarRow(m.cat.Get("sidebar.no_feeds"), 0, false))
	} else {
		for i, f := range m.feeds {
			b.WriteString(m.sidebarRow(f.Label, m.counts[f.Key].Unread,
				i == m.sidebarSel && m.focus == regionSidebar))
			if i < len(m.feeds)-1 {
				b.WriteString("\n")
			}
		}
	}
	return m.th.Sidebar.
		Width(sidebarWidth).
		Height(m.bodyHeight()).
		Render(b.String())
}

// sidebarRow renders one sidebar line: " label…  N" with the unread
// badge, truncated to the inner width so a narrow terminal never wraps.
func (m Model) sidebarRow(label string, unread int, selected bool) string {
	inner := sidebarWidth - 2 // left border + padding
	badge := ""
	if unread > 0 {
		badge = strconv.Itoa(unread)
	}
	labelW := inner - 1 - len(badge)
	if badge != "" {
		labelW-- // gap between label and badge
	}
	if labelW < 1 {
		labelW = 1
	}
	name := runewidth.FillRight(runewidth.Truncate(label, labelW, "…"), labelW)
	row := " " + name
	if badge != "" {
		styled := m.th.SidebarBadge.Render(badge)
		if selected {
			styled = m.th.SidebarSelected.Render(badge)
		}
		row += " " + styled
	}
	if selected {
		return m.th.SidebarSelected.Render(row)
	}
	return m.th.SidebarItem.Render(row)
}

func (m Model) statusBarView() string {
	left := m.th.StatusNormal.Render(m.sourceLabel(m.active))
	if f := m.selectedFeed(); f != nil {
		if c := m.counts[f.Key]; c.Total > 0 {
			parts := []string{m.cat.Get("status.items", c.Total)}
			if c.Unread > 0 {
				parts = append(parts, m.cat.Get("status.new", c.Unread))
			}
			left = m.th.StatusNormal.Render(f.Label + " · " + strings.Join(parts, " · "))
		} else {
			left = m.th.StatusNormal.Render(f.Label)
		}
	}
	if m.refreshing {
		left = m.th.StatusAccent.Render(m.cat.Get("status.refreshing")) + "  " + left
	}
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

// refreshStatus localizes a finished refresh: per-feed failures surface
// the summary line, otherwise the new-item count. The source is taken
// from the message, not the model — the user may have Tabbed away while
// the fetch ran.
func (m Model) refreshStatus(msg refreshDoneMsg) string {
	switch {
	case msg.err != nil:
		return m.cat.Get("rss.feed_error", msg.source, msg.err)
	case len(msg.results) == 0:
		return refresh.NoFeedsHint(m.cat)
	}
	newSum, failed := 0, 0
	for _, r := range msg.results {
		if r.Err != nil {
			failed++
			continue
		}
		newSum += r.New
	}
	if failed > 0 {
		return m.cat.Get("rss.summary", len(msg.results), len(msg.results)-failed, failed)
	}
	return m.cat.Get("status.refresh_done", newSum)
}

func (m Model) helpView() string {
	rows := make([]string, 0, len(helpBindings))
	for _, b := range helpBindings {
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
