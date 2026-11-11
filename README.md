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

Not published yet — early milestones. From source (Go 1.24+):

```bash
git clone https://github.com/smy-101/readscope.git
cd readscope
go build ./cmd/readscope
```

RSS daily reading already works (M2); Hacker News (M3) and the EPUB library (M4) land next.

## Usage

```bash
readscope                 # start the TUI (default)
readscope doctor          # environment self-check, incl. per-feed reachability
readscope rss refresh     # fetch every subscription into the offline cache
readscope rss import F    # merge OPML subscriptions into sources.toml
readscope rss export F    # write subscriptions as OPML 2.0 for other readers
readscope render <file|url>  # render HTML to terminal text (pipeline debug)
readscope version         # print version
```

### RSS

Subscriptions live in `sources.toml`, next to `config.toml` (`readscope doctor` prints both paths):

```toml
enabled = ["rss"]

[rss.feeds]
ruanyifeng = { label = "阮一峰周刊", url = "https://www.ruanyifeng.com/blog/atom.xml" }
hn = { label = "Hacker News", url = "https://hnrss.org/frontpage" }
```

The map key is a stable slug (used in item ids and the sidebar); `label` is optional and
falls back to the key. Refreshes send conditional requests (ETag / If-Modified-Since), so
unchanged feeds cost a 304 and skip parsing; items are snapshotted into SQLite — lists
paint from cache and full text stays readable offline. In the TUI, `F5` refreshes in the
background.

Moving in or out of another reader is OPML all the way: `readscope rss import subs.opml`
merges (existing subscriptions untouched), `readscope rss export subs.opml` writes a
standard OPML 2.0 document.

`readscope doctor` probes every subscription with the same User-Agent and reports
✓ ok (with elapsed time), ! slow (> 3 s, warning) or ✗ failed (exit code 1) — a quick way
to tell a dead feed from a broken setup.

### Keys

Every binding is a non-printing key, so a Chinese IME never swallows it:

| Key | Action | Key | Action |
|-----|--------|-----|--------|
| Tab / Shift+Tab | Switch source | F5 | Refresh |
| ← → ↑ ↓ | Move / scroll | Ctrl+R | Toggle read/unread |
| Enter | Open / enter | Ctrl+A | Mark all read (confirms) |
| Esc | Back one layer | Ctrl+F | Star/unstar |
| F1 | This help | Ctrl+O | Open in browser |
| F3 | Favorites | Ctrl+Y | Copy link |
| F4 | Unread-only view | Ctrl+T | Switch theme |
| | | Ctrl+Q | Quit |

(F2, subscription management inside the TUI, comes in a later milestone.)

## Development

```bash
go build ./...       # build
go test ./...        # tests
go vet ./...
```

Design doc: [docs/PLAN.md](docs/PLAN.md) (中文).

## License

[MIT](LICENSE)
