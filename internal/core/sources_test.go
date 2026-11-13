package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSourcesMissingFileGivesDefault(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	srcs, err := LoadSources()
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	// Fresh installs ship every list source: rss needs subscriptions,
	// hn and zhihu work with none (fixed feeds; hot is anonymous) —
	// zero-config readable.
	if len(srcs.Enabled) != 4 || srcs.Enabled[0] != "rss" || srcs.Enabled[1] != "hn" || srcs.Enabled[2] != "zhihu" || srcs.Enabled[3] != "heybox" {
		t.Fatalf("Enabled = %v, want [rss hn zhihu heybox]", srcs.Enabled)
	}
	if len(srcs.RSS.Feeds) != 0 {
		t.Fatalf("Feeds = %v, want empty", srcs.RSS.Feeds)
	}
}

func TestLoadSourcesFileWithoutEnabledDefaultsRSS(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", dir)
	body := "[rss.feeds]\nblog = { label = \"Blog\", url = \"https://x.example/feed\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "sources.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := LoadSources()
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(srcs.Enabled) != 4 || srcs.Enabled[0] != "rss" || srcs.Enabled[1] != "hn" || srcs.Enabled[2] != "zhihu" || srcs.Enabled[3] != "heybox" {
		t.Fatalf("Enabled = %v, want [rss hn zhihu heybox] default", srcs.Enabled)
	}
	if f := srcs.RSS.Feeds["blog"]; f.Label != "Blog" || f.URL != "https://x.example/feed" {
		t.Fatalf("feed blog = %+v", f)
	}
}

func TestLoadSourcesExplicitEmptyEnabledHonored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "sources.toml"), []byte("enabled = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := LoadSources()
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(srcs.Enabled) != 0 {
		t.Fatalf("Enabled = %v, want empty", srcs.Enabled)
	}
}

func TestLoadSourcesMalformedFileErrorsWithPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "sources.toml"), []byte("enabled = [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSources()
	if err == nil {
		t.Fatal("expected error for malformed sources.toml")
	}
	if !strings.Contains(err.Error(), "sources.toml") {
		t.Fatalf("error should name the file, got: %v", err)
	}
}

func TestSourcesSaveLoadRoundtrip(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	srcs := DefaultSources()
	srcs.RSS.Feeds = map[string]RSSFeed{
		"ruanyq": {Label: "阮一峰周刊", URL: "https://www.ruanyifeng.com/blog/atom.xml"},
		"blog":   {URL: "https://example.com/rss.xml"}, // label omitted → key shown
	}
	if err := srcs.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadSources()
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(got.Enabled) != 4 || got.Enabled[0] != "rss" || got.Enabled[1] != "hn" || got.Enabled[2] != "zhihu" || got.Enabled[3] != "heybox" {
		t.Fatalf("Enabled = %v, want [rss hn zhihu heybox]", got.Enabled)
	}
	if len(got.RSS.Feeds) != 2 {
		t.Fatalf("Feeds = %v, want 2", got.RSS.Feeds)
	}
	if f := got.RSS.Feeds["ruanyq"]; f.Label != "阮一峰周刊" ||
		f.URL != "https://www.ruanyifeng.com/blog/atom.xml" {
		t.Fatalf("feed ruanyq = %+v", f)
	}
	if f := got.RSS.Feeds["blog"]; f.Label != "" || f.URL != "https://example.com/rss.xml" {
		t.Fatalf("feed blog = %+v", f)
	}
	// The default state carries no [heybox] table — the marshaled file
	// must stay clean of it (nil pointer, not an empty table), so a
	// fresh Save never grows a section nobody configured.
	if got.Heybox != nil {
		t.Fatalf("Heybox = %+v, want nil — no circles means no table", got.Heybox)
	}
	raw, err := os.ReadFile(mustSourcesPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[heybox]") {
		t.Fatalf("default Save wrote a [heybox] section:\n%s", raw)
	}
}

func mustSourcesPath(t *testing.T) string {
	t.Helper()
	path, err := SourcesPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSourcesHeyboxCirclesRoundtrip pins the circle list's two load-
// bearing properties: declaration order survives a Save/Load cycle
// (the sidebar order is the file's order), and a hand-written file
// parses into the same shape the CLI writes — either direction of
// the “TOML 手改永远兜底” promise.
func TestSourcesHeyboxCirclesRoundtrip(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	srcs := DefaultSources()
	srcs.Heybox = &HeyboxConfig{Circles: []HeyboxCircle{
		{Name: "黑神话：悟空", TopicID: 568547},
		{Name: "塞尔达传说", TopicID: 42},
	}}
	if err := srcs.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadSources()
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if got.Heybox == nil || len(got.Heybox.Circles) != 2 {
		t.Fatalf("Heybox = %+v, want 2 circles", got.Heybox)
	}
	if got.Heybox.Circles[0].Name != "黑神话：悟空" || got.Heybox.Circles[0].TopicID != 568547 {
		t.Fatalf("circle[0] = %+v, want 黑神话：悟空/568547 first", got.Heybox.Circles[0])
	}
	if got.Heybox.Circles[1].Name != "塞尔达传说" || got.Heybox.Circles[1].TopicID != 42 {
		t.Fatalf("circle[1] = %+v, want 塞尔达传说/42 second", got.Heybox.Circles[1])
	}
}

// TestLoadSourcesHandEditedCircles: a hand-written array-of-tables
// (the edit-a-circle-out, reorder, re-add workflow) parses with order
// and ids intact.
func TestLoadSourcesHandEditedCircles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", dir)
	body := `enabled = ["rss", "heybox"]

[[heybox.circles]]
name = "塞尔达传说"
topic_id = 42

[[heybox.circles]]
name = "黑神话：悟空"
topic_id = 568547
`
	if err := os.WriteFile(filepath.Join(dir, "sources.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := LoadSources()
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if srcs.Heybox == nil || len(srcs.Heybox.Circles) != 2 {
		t.Fatalf("Heybox = %+v, want 2 circles", srcs.Heybox)
	}
	if srcs.Heybox.Circles[0].TopicID != 42 || srcs.Heybox.Circles[1].TopicID != 568547 {
		t.Fatalf("order = [%d %d], want the file's order [42 568547]",
			srcs.Heybox.Circles[0].TopicID, srcs.Heybox.Circles[1].TopicID)
	}
}
