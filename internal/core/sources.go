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
// beyond RSS grow their own tables here as they land (hn in M3).
type Sources struct {
	Enabled []string  `toml:"enabled"`
	RSS     RSSConfig `toml:"rss"`
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
// the RSS source enabled, with no feeds. A fresh install therefore
// renders the TUI happily and `rss refresh` says "no feeds" instead of
// erroring.
func DefaultSources() *Sources {
	return &Sources{Enabled: []string{"rss"}}
}

// LoadSources reads sources.toml; a missing file yields the default.
// Like Config, a malformed file is an error naming the path, never
// silently ignored. An explicit empty `enabled = []` is honored; a file
// that omits the key entirely falls back to ["rss"].
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
