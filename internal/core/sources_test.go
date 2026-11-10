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
	if len(srcs.Enabled) != 1 || srcs.Enabled[0] != "rss" {
		t.Fatalf("Enabled = %v, want [rss]", srcs.Enabled)
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
	if len(srcs.Enabled) != 1 || srcs.Enabled[0] != "rss" {
		t.Fatalf("Enabled = %v, want [rss] default", srcs.Enabled)
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
	if len(got.Enabled) != 1 || got.Enabled[0] != "rss" {
		t.Fatalf("Enabled = %v, want [rss]", got.Enabled)
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
}
