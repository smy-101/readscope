# readscope

> Terminal reading scope — RSS feeds, Hacker News and your local EPUB library, all in one terminal.

**Language**: English · [中文](README.zh-CN.md)

Status: **0.x, work in progress.** readscope is a terminal-first reader written in Go: a feed
stream (RSS/Atom subscriptions + Hacker News) and a local EPUB bookshelf behind one keyboard-only,
IME-safe UI. Inspired by [FeedScope](https://github.com/howardli/FeedScope).

## Features (planned for v0.1)

- **Feeds**: RSS/Atom subscriptions with OPML import/export; Hacker News via the public Algolia API — no login, no API key
- **EPUB bookshelf**: scan a library directory, `add` copies books in; read in a dedicated full-screen mode with TOC, bookmarks and full-text search
- **State that survives**: read marks, favorites (full snapshots), and reading progress persist in SQLite; cached lists paint instantly and stay readable offline
- **CJK-aware rendering**: one shared HTML→terminal pipeline renders both feed articles and EPUB chapters, with Chinese-aware line wrapping and code highlighting
- **IME-safe keys**: every binding is a non-printing key (Tab / arrows / F-keys) — works while a Chinese IME is active
- **Bilingual UI** (中文 / English) and dark/light themes

Not in v0: login-gated sources (Zhihu / X / Reddit scraping), in-terminal image rendering, highlights export, mobi.

## Install

Not published yet — M0 in progress. From source (Go 1.24+):

```bash
git clone https://github.com/smy-101/readscope.git
cd readscope
go build ./cmd/readscope
```

## Usage

```bash
readscope            # start the TUI (default)
readscope doctor     # environment self-check
```

## Development

```bash
go build ./...       # build
go test ./...        # tests
go vet ./...
```

Design doc: [docs/PLAN.md](docs/PLAN.md) (中文).

## License

[MIT](LICENSE)
