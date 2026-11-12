package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(t *testing.T) Model {
	return seededModel(t, nil, nil)
}

func size(m Model, w, h int) Model {
	m2, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m2.(Model)
}

func TestViewRendersShell(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	view := m.View()
	for _, want := range []string{
		"Bookshelf", "RSS", "Hacker News", // source bar
		"no feeds yet", // sidebar
		"No items yet", // list
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) != 30 {
		t.Errorf("view height = %d lines, want 30", len(lines))
	}
}

func TestTabRotatesSources(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != 1 || m.focus != regionSidebar {
		t.Fatalf("after Tab: active=%d focus=%v", m.active, m.focus)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyTab}) // hn → zhihu
	if m.active != len(sourceIDs)-1 {
		t.Fatalf("Tab should reach the last source, active=%d", m.active)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyTab}) // zhihu → books wraps
	if m.active != 0 {
		t.Fatalf("Tab should wrap to first source, active=%d", m.active)
	}
}

func updateKey(m Model, msg tea.KeyMsg) Model {
	m2, _ := m.Update(msg)
	return m2.(Model)
}

// keyCmd presses a key keeping the returned command, for flows whose
// loading chain the test then pumps.
func keyCmd(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	m2, cmd := m.Update(msg)
	return m2.(Model), cmd
}

func TestHelpOverlay(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyF1})
	view := m.View()
	if !strings.Contains(view, "Keybindings") {
		t.Fatalf("help overlay missing title:\n%s", view)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(m.View(), "Keybindings") {
		t.Fatal("Esc should close the help overlay")
	}
}

// TestHelpOverlayListsM2Keys pins ticket 07's acceptance: the F1
// overlay renders every M2 binding with its description, so a binding
// dropped from helpBindings (or a broken i18n key) fails here.
func TestHelpOverlayListsM2Keys(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyF1})
	view := m.View()
	for _, want := range []string{
		"Enter", "Open / enter",
		"Esc", "Back one layer",
		"F3", "Favorites",
		"F4", "Unread-only view",
		"F5", "Refresh",
		"Ctrl+R", "Toggle read/unread",
		"Ctrl+A", "Mark all read",
		"Ctrl+F", "Star/unstar",
		"Ctrl+O", "Open in browser",
		"Ctrl+Y", "Copy link",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("help overlay missing %q", want)
		}
	}
}

func TestCtrlTCyclesThemeAndPersists(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.th.Name != "light" {
		t.Fatalf("Ctrl+T should switch dark→light, got %q", m.th.Name)
	}
	if m.cfg.Theme != "light" {
		t.Fatalf("theme choice not persisted in config, got %q", m.cfg.Theme)
	}
	if !strings.Contains(m.status, "light") {
		t.Fatalf("status should mention the new theme: %q", m.status)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlQ}, {Type: tea.KeyCtrlC}} {
		m := size(newTestModel(t), 100, 30)
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%v should request quit", k.Type)
		}
	}
}
