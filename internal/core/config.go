// Package core holds readscope's domain: configuration now, models
// (Item, Feed, Book) as the milestones land.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Config is the user-facing configuration file (~/.config/readscope/config.toml).
// Subscriptions live separately in sources.toml (M2).
type Config struct {
	Language  string `toml:"language"`   // "en" (default) | "zh-CN"
	Theme     string `toml:"theme"`      // "dark" (default) | "light"
	BookDir   string `toml:"book_dir"`   // EPUB library directory (M4)
	TextWidth int    `toml:"text_width"` // 0 = follow terminal width
}

// Dir returns the configuration directory: $READSCOPE_CONFIG, else the
// OS config dir + "readscope".
func Dir() (string, error) {
	if dir := os.Getenv("READSCOPE_CONFIG"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "readscope"), nil
}

// Path returns the config file location (for doctor).
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads the config file; a missing file yields defaults (locale-detected
// language, dark theme). A malformed file is an error, never silently ignored.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	cfg := Defaults()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Defaults builds the fallback configuration. Language is detected from the
// locale (zh* → zh-CN), matching the first-run pre-selection FeedScope uses.
func Defaults() *Config {
	return &Config{
		Language:  detectLanguage(),
		Theme:     "dark",
		BookDir:   "~/Books",
		TextWidth: 0,
	}
}

// Save writes the config back to disk (mkdir -p first).
func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	return os.WriteFile(path, raw, 0o644)
}

func detectLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" && strings.HasPrefix(strings.ToLower(v), "zh") {
			return "zh-CN"
		}
	}
	return "en"
}
