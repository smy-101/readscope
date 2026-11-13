package core

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// Sources is the subscription file (sources.toml, next to config.toml):
// which sources are enabled — the list order is the source rail order in
// the TUI — and their per-source subscription config. Feeds for sources
// beyond RSS grow their own tables here as they land (hn in M3, heybox
// circles in M5).
type Sources struct {
	Enabled []string      `toml:"enabled"`
	RSS     RSSConfig     `toml:"rss"`
	Heybox  *HeyboxConfig `toml:"heybox"` // nil = no circles yet; absent from the marshaled file
}

// RSSConfig holds the RSS/Atom subscriptions: map key = feed key (stable
// slug used in item ids and the sidebar).
type RSSConfig struct {
	Feeds map[string]RSSFeed `toml:"feeds"`
}

// RSSFeed is one RSS/Atom subscription.
type RSSFeed struct {
	Label string `toml:"label"` // display name; empty → the key is shown
	URL   string `toml:"url"`   // feed address (RSS or Atom; auto-detected)
}

// HeyboxConfig holds the heybox (小黑盒) circle subscriptions (M5
// ticket 02). Circles ride an array-of-tables, not a map, for one
// reason: declaration order is the sidebar order, and a hand edit of
// the list (reorder, delete, comment out) must keep working — the
// GLOSSARY's “TOML 手改永远兜底” promise.
type HeyboxConfig struct {
	Circles []HeyboxCircle `toml:"circles"`
}

// HeyboxCircle is one circle (圈) subscription. TopicID is the
// circle's identity on the host and the feed key's spine
// ("circle/<topic_id>") — stable across renames; Name is display
// only. The search CLI fills both; a hand edit may fix either.
type HeyboxCircle struct {
	Name    string `toml:"name"`
	TopicID int64  `toml:"topic_id"`
}

// HeyboxCircles returns the subscribed circles in declaration order,
// empty when no [heybox] table exists — the nil-safe read every
// consumer (source construction, the add CLI, doctor's census)
// starts from.
func (s *Sources) HeyboxCircles() []HeyboxCircle {
	if s == nil || s.Heybox == nil {
		return nil
	}
	return s.Heybox.Circles
}

// SourcesPath returns the subscription file location (for messages and
// doctor).
func SourcesPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sources.toml"), nil
}

// DefaultSources is the subscription state when no file exists yet:
// every list source enabled, in rail order — rss starts with no
// feeds (add some), hn, zhihu and heybox need none (their feeds are
// fixed; zhihu's hot board and digest are anonymous, heybox's
// recommend feed too — circles arrive via `heybox add` into the
// [heybox] table, absent until then). A fresh install therefore
// renders the TUI happily, reads HN, the zhihu hot board and the
// heybox recommend feed after one F5, and `rss refresh` says "no
// feeds" instead of erroring.
func DefaultSources() *Sources {
	return &Sources{Enabled: []string{"rss", "hn", "zhihu", "heybox"}}
}

// LoadSources reads sources.toml; a missing file yields the default.
// Like Config, a malformed file is an error naming the path, never
// silently ignored. An explicit empty `enabled = []` is honored; a file
// that omits the key entirely falls back to the default pair. An
// existing sources.toml is never rewritten — upgrading installs add
// "hn" to enabled by hand (README documents the line).
func LoadSources() (*Sources, error) {
	path, err := SourcesPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultSources(), nil
	}
	if err != nil {
		return nil, err
	}
	srcs := DefaultSources()
	if err := toml.Unmarshal(raw, srcs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return srcs, nil
}

// Save writes the subscriptions back to disk (mkdir -p first), for the
// OPML import path (M2, ticket 06).
func (s *Sources) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := toml.Marshal(s)
	if err != nil {
		return err
	}
	path, err := SourcesPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
