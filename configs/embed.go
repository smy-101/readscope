// Package configs embeds the static configuration data shipped inside the
// readscope binary: language packs today, theme files later.
package configs

import "embed"

// I18n exposes the embedded language packs, one TOML file per language
// under i18n/ (e.g. i18n/zh-CN.toml). New languages are picked up by just
// dropping a file in — no code changes.
//
//go:embed i18n/*.toml
var I18n embed.FS
