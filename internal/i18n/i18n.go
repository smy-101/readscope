// Package i18n loads embedded language packs (one TOML file per language)
// and serves messages by dot-separated key. A parity test keeps every
// catalog's key tree identical, so a missing translation fails CI rather
// than shipping.
package i18n

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/smy-101/readscope/configs"
)

// Default is the fallback language code, used when Load receives an
// unknown code (missing keys fall through to the key itself — loud, not
// silent).
const Default = "en"

// Info describes one embedded language pack.
type Info struct {
	Code string // "zh-CN"
	Name string // native name from [meta], e.g. "简体中文"
}

// Catalog is one loaded language.
type Catalog struct {
	Code string
	Name string
	msgs map[string]string
}

// Available lists the embedded language packs, sorted by code.
func Available() ([]Info, error) {
	entries, err := fs.Glob(configs.I18n, "i18n/*.toml")
	if err != nil {
		return nil, err
	}
	infos := make([]Info, 0, len(entries))
	for _, entry := range entries {
		code := strings.TrimSuffix(path.Base(entry), ".toml")
		inf, err := peekMeta(entry)
		if err != nil {
			return nil, fmt.Errorf("language pack %s: %w", entry, err)
		}
		infos = append(infos, Info{Code: code, Name: inf.Name})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Code < infos[j].Code })
	return infos, nil
}

// Load reads the pack for code; an unknown code falls back to Default.
func Load(code string) (*Catalog, error) {
	for _, candidate := range []string{code, Default} {
		name := "i18n/" + candidate + ".toml"
		raw, err := configs.I18n.ReadFile(name)
		if err != nil {
			continue
		}
		c, err := parse(candidate, raw)
		if err != nil {
			return nil, fmt.Errorf("language pack %s: %w", name, err)
		}
		return c, nil
	}
	return nil, fmt.Errorf("no language pack found (wanted %q, default %q missing)", code, Default)
}

// Get returns the message for key, formatted with optional fmt.Sprintf args.
// A missing key returns the key itself so bugs are visible, never silent.
func (c *Catalog) Get(key string, args ...any) string {
	msg, ok := c.msgs[key]
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// Keys returns all message keys, sorted (used by the parity test).
func (c *Catalog) Keys() []string {
	keys := make([]string, 0, len(c.msgs))
	for k := range c.msgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parse(code string, raw []byte) (*Catalog, error) {
	var tree map[string]any
	if err := toml.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	c := &Catalog{Code: code, msgs: map[string]string{}}
	if meta, ok := tree["meta"].(map[string]any); ok {
		if name, ok := meta["name"].(string); ok {
			c.Name = name
		}
	}
	delete(tree, "meta")
	flatten(tree, "", c.msgs)
	if c.Name == "" {
		return nil, fmt.Errorf("[meta] name is required")
	}
	return c, nil
}

// flatten walks nested TOML tables into dot-separated keys ("help.keys.quit").
func flatten(tree map[string]any, prefix string, out map[string]string) {
	for k, v := range tree {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			flatten(val, key, out)
		case string:
			out[key] = val
		default:
			out[key] = fmt.Sprintf("%v", val)
		}
	}
}

func peekMeta(entry string) (Info, error) {
	raw, err := configs.I18n.ReadFile(entry)
	if err != nil {
		return Info{}, err
	}
	c, err := parse(path.Base(entry), raw)
	if err != nil {
		return Info{}, err
	}
	return Info{Code: c.Code, Name: c.Name}, nil
}
