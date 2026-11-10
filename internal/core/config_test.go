package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileGivesLocaleDefaults(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	t.Setenv("LC_ALL", "en_US.UTF-8")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Language != "en" {
		t.Fatalf("Language = %q, want en", cfg.Language)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("Theme = %q, want dark", cfg.Theme)
	}
}

func TestDetectLanguageChineseLocale(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Language != "zh-CN" {
		t.Fatalf("Language = %q, want zh-CN", cfg.Language)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	cfg := Defaults()
	cfg.Theme = "light"
	cfg.TextWidth = 100
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Theme != "light" || got.TextWidth != 100 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestLoadMalformedFileErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("theme = [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed config")
	}
}
