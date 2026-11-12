package ui

// Ticket 04 tests: the Ctrl+U login dialog — open, paste (typed runes
// and the one-big-KeyRunes bracketed-paste shape), the masked view
// (the raw header must never reach the screen or scrollback), save →
// 0600 file via the shared SaveCookie seam, refusal keeping the
// dialog open — plus the status-bar mapping of the zhihu session
// errors (失效语义): expired → re-login guidance, no cookie → log-in
// guidance, raw HTTP errors stay raw.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/refresh"
	"github.com/smy-101/readscope/internal/source/zhihu"
)

const loginPaste = "z_c0=2+1.0x/secret; d_c0=2+1.0x/dev"

// pasteKeys sends the buffer as one bracketed-paste KeyRunes msg —
// the shape bubbletea delivers for a terminal paste.
func pasteKeys(m Model, s string) Model {
	return updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true})
}

func cookieFilePath(t *testing.T) string {
	t.Helper()
	path, err := zhihu.CookiePath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCtrlUDialogSavesCookie(t *testing.T) {
	// seededModel points READSCOPE_CONFIG at a temp dir — the save
	// lands there, never in the developer's real config.
	m := size(newTestModel(t), 100, 30)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if !m.loginOpen {
		t.Fatal("Ctrl+U should open the login dialog")
	}
	view := m.View()
	if !strings.Contains(view, "Zhihu Login") {
		t.Fatalf("dialog missing title:\n%s", view)
	}

	// Paste, then Enter: the save runs through the same SaveCookie
	// seam the CLI rides; success closes the dialog and reports the
	// census; the file is 0600 and normalized.
	m = pasteKeys(m, loginPaste)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginOpen {
		t.Fatal("successful save should close the dialog")
	}
	if !strings.Contains(m.status, "cookie saved") || !strings.Contains(m.status, "z_c0") {
		t.Fatalf("status = %q, want the saved census", m.status)
	}
	path := cookieFilePath(t)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cookie file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 0600", perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != loginPaste {
		t.Errorf("stored = %q, want the normalized header", raw)
	}
}

func TestCtrlUDialogMasksThePaste(t *testing.T) {
	// The terminal keeps scrollback: rendering the pasted header would
	// leave a live session in it. The view shows dots and the title,
	// never the header itself.
	m := size(newTestModel(t), 100, 30)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = pasteKeys(m, loginPaste)
	view := m.View()
	if strings.Contains(view, loginPaste) || strings.Contains(view, "2+1.0x") {
		t.Fatalf("raw cookie leaked into the view:\n%s", view)
	}
	if !strings.Contains(view, "•") {
		t.Fatalf("masked buffer missing dots:\n%s", view)
	}
	// Esc cancels without saving anything.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.loginOpen {
		t.Fatal("Esc should close the dialog")
	}
	if _, err := os.Stat(cookieFilePath(t)); !os.IsNotExist(err) {
		t.Errorf("cancel must not write a file: %v", err)
	}
}

func TestCtrlURefusalKeepsDialogOpen(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = pasteKeys(m, "hello world") // no z_c0: the ceremony's one refusal
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginOpen {
		t.Fatal("a refused paste should keep the dialog open for a re-paste")
	}
	if !strings.Contains(m.status, "invalid cookie") || !strings.Contains(m.status, "z_c0") {
		t.Fatalf("status = %q, want the refusal naming z_c0", m.status)
	}
	if _, err := os.Stat(cookieFilePath(t)); !os.IsNotExist(err) {
		t.Errorf("refusal must not write a file: %v", err)
	}

	// Refusal cleared the buffer — the next paste starts clean; no
	// in-dialog select-all exists to fix a prefixed garbage buffer by
	// hand. Paste the good header over the refusal and save.
	m = pasteKeys(m, loginPaste)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginOpen || !strings.Contains(m.status, "cookie saved") {
		t.Fatalf("re-paste should save and close: open=%v status=%q", m.loginOpen, m.status)
	}
	if got, _ := os.ReadFile(cookieFilePath(t)); string(got) != loginPaste {
		t.Errorf("stored = %q, want the clean re-paste", got)
	}
}

func TestCtrlUEnterOnEmptyBufferIsNoop(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginOpen {
		t.Fatal("Enter on an empty paste is a no-op, not a save or close")
	}
	if _, err := os.Stat(cookieFilePath(t)); !os.IsNotExist(err) {
		t.Errorf("empty paste must not write a file: %v", err)
	}
}

func TestHelpListsCtrlU(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyF1})
	view := m.View()
	if !strings.Contains(view, "Ctrl+U") || !strings.Contains(view, "zhihu login") {
		t.Fatalf("F1 overlay missing the Ctrl+U row:\n%s", view)
	}
}

// --- 失效语义: session errors → status guidance ---------------------------

func TestRefreshStatusMapsSessionErrors(t *testing.T) {
	m := newTestModel(t)

	expired := refreshDoneMsg{results: []refresh.Result{{
		Source: "zhihu", Feed: "follow", Label: "Follow",
		Err: fmt.Errorf("fetch follow: %w", zhihu.ErrSessionExpired),
	}}}
	if got := m.refreshStatus(expired); !strings.Contains(got, "login expired") || !strings.Contains(got, "Ctrl+U") {
		t.Errorf("expired session: status = %q, want re-login guidance", got)
	}

	required := refreshDoneMsg{results: []refresh.Result{{
		Source: "zhihu", Feed: "follow", Label: "Follow",
		Err: fmt.Errorf("fetch follow: %w", zhihu.ErrLoginRequired),
	}}}
	if got := m.refreshStatus(required); !strings.Contains(got, "not logged in") || !strings.Contains(got, "Ctrl+U") {
		t.Errorf("no cookie: status = %q, want log-in guidance", got)
	}

	// Raw HTTP stays raw: the v3 risk-control branch never claims
	// session state.
	plain := refreshDoneMsg{results: []refresh.Result{{
		Source: "zhihu", Feed: "follow", Label: "Follow",
		Err: errors.New("fetch follow: moments: HTTP 403"),
	}}}
	got := m.refreshStatus(plain)
	if !strings.Contains(got, "HTTP 403") || strings.Contains(got, "login") {
		t.Errorf("plain 403: status = %q, want the raw error", got)
	}

	// Multi-feed failures keep the summary shape (no single error to
	// promote); all-green keeps the new-item count.
	multi := refreshDoneMsg{results: []refresh.Result{
		{Source: "rss", Feed: "a", Label: "A", Err: errors.New("boom")},
		{Source: "rss", Feed: "b", Label: "B", New: 3, Total: 5},
	}}
	if got := m.refreshStatus(multi); !strings.Contains(got, "1 failed") && !strings.Contains(got, "1 失败") {
		t.Errorf("multi-feed failure should summarize, got %q", got)
	}
}

// The gated preflight reaches the UI through the refresh pipeline as
// any other fetch error — pinned end to end here: a source whose
// fetch returns ErrLoginRequired renders the log-in guidance line in
// the status bar after F5, and hot/daily feeds are untouched by the
// state (they are anonymous; their fetches never carry the error).
func TestRefreshDoneLoginGuidanceLandsInStatusBar(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	res, cmd := m.Update(refreshDoneMsg{source: "zhihu", results: []refresh.Result{{
		Source: "zhihu", Feed: "follow", Label: "Follow",
		Err: fmt.Errorf("fetch follow: %w", zhihu.ErrLoginRequired),
	}}})
	m2, ok := res.(Model)
	if !ok {
		t.Fatalf("Update returned %T", res)
	}
	m2 = pump(t, m2, cmd) // loadFeedsCmd chain
	if !strings.Contains(m2.status, "Ctrl+U") {
		t.Fatalf("status bar should carry login guidance: %q", m2.status)
	}
	_ = m
}

// TestLoginFileNeverEchoedInDialogOutput is the scrollback contract
// stated once more from the outside: the dialog's full rendered view
// (title, hint, mask, footer) contains no fragment of the secret —
// not even a truncated one.
func TestLoginFileNeverEchoedInDialogOutput(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = pasteKeys(m, "z_c0=VERYSUPERSECRETTOKEN1234; d_c0=alsodev")
	view := m.View()
	for _, fragment := range []string{"VERYSUPERSECRET", "alsodev", "z_c0="} {
		if strings.Contains(view, fragment) {
			t.Errorf("view leaked %q:\n%s", fragment, view)
		}
	}
}

// silence the unused-helper warning for context/filepath if the file
// evolves; keeps imports honest as tests narrow.
var _ = context.Background
var _ = filepath.Join
