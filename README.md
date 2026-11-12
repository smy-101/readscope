# readscope

> Terminal reading scope — RSS feeds, Hacker News and your local EPUB library, all in one terminal.

**Language**: English · [中文](README.zh-CN.md)

Status: **0.x, work in progress.** readscope is a terminal-first reader written in Go: a feed
stream (RSS/Atom subscriptions + Hacker News) and a local EPUB bookshelf behind one keyboard-only,
IME-safe UI. Inspired by [FeedScope](https://github.com/howardli/FeedScope).

## Features (planned for v0.1)

- **Feeds**: RSS/Atom subscriptions with OPML import/export; Hacker News via the public Algolia API — no login, no API key; Zhihu hot board and daily digest anonymously, recommend feed, follow feed and answer comments with a pasted browser cookie
- **EPUB bookshelf**: scan a library directory, `add` copies books in; read in a dedicated full-screen mode with TOC, bookmarks and full-text search
- **State that survives**: read marks, favorites (full snapshots), and reading progress persist in SQLite; cached lists paint instantly and stay readable offline
- **CJK-aware rendering**: one shared HTML→terminal pipeline renders both feed articles and EPUB chapters, with Chinese-aware line wrapping and code highlighting
- **IME-safe keys**: every binding is a non-printing key (Tab / arrows / F-keys) — works while a Chinese IME is active
- **Bilingual UI** (中文 / English) and dark/light themes

Not in v0: other login-gated sources (X / Reddit scraping), in-terminal image rendering, highlights export, mobi.

## Install

Not published yet — early milestones. From source (Go 1.24+):

```bash
git clone https://github.com/smy-101/readscope.git
cd readscope
go build ./cmd/readscope
```

RSS daily reading (M2), Hacker News (M3) and the Zhihu source (M4) already work; the EPUB library (M5) lands next.

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
enabled = ["rss", "hn", "zhihu"]

[rss.feeds]
ruanyifeng = { label = "阮一峰周刊", url = "https://www.ruanyifeng.com/blog/atom.xml" }
hn = { label = "Hacker News", url = "https://hnrss.org/frontpage" }
```

The map key is a stable slug (used in item ids and the sidebar); `label` is optional and
falls back to the key. Refreshes send conditional requests (ETag / If-Modified-Since), so
unchanged feeds cost a 304 and skip parsing; items are snapshotted into SQLite — lists
paint from cache and full text stays readable offline. In the TUI, `F5` refreshes in the
background.

> Upgrading from an older install? `sources.toml` is never rewritten automatically —
> add `"hn"` and `"zhihu"` to your `enabled` list by hand to turn on the sources below.

### Hacker News

Tab over to Hacker News for four fixed feeds — Front / New / Ask HN / Show HN — served by
the public [Algolia HN Search API](https://hn.algolia.com) (no account, no key). `F5`
refreshes the current feed into the same offline cache as RSS: stories snapshot to
SQLite, the unread dot / read / star mechanics all work identically, and everything
stays readable offline. `Ctrl+N` appends the next Algolia page to the list (no duplicate
rows, read state kept); inside a post it pages floors and replies in memory. Reading a
post runs three layers deep — body, floors (one per top-level comment), then a floor's
reply tree — with deep reply chains flattened to two levels and prefixed `@parent`, so
even a narrow terminal keeps who-replied-to-whom readable. HN refreshes are always
plain refetches — the index has no conditional-request semantics. Link posts store the
article link as their body (open it with `Ctrl+O`); text posts (Ask/Show) carry their own
HTML through the same render pipeline as RSS bodies. Note the Algolia index lags the site by a few minutes and ranks
by points/date, not by HN's official ordering — readscope does not promise official
ranking.

### Zhihu

Tab over to Zhihu for three feeds. The **hot board** (热榜) and the **daily digest** (知乎日报) are
anonymous — no account, no cookie — served by `api.zhihu.com` and `daily.zhihu.com`. The hot board
is the whole-site trending list: a fixed ~30 question cards ranked by heat ("2045 万热度" in the
meta column), `Ctrl+N` reports the end. The daily digest is one day's issue, its stories fetched
as full text on first open and then snapshotted — readable offline. Both land in the same offline
cache as RSS and HN: the unread dot / read / star mechanics all work identically.

The **follow feed** (关注流) needs a login: paste your browser cookie once (below) and it lists the
question cards from the people you follow, cursor-paged by `Ctrl+N`. Opening a question reads it
in three layers, HN-style: the question layer, then the answer layer — `Enter` on a floor reads
that answer's full text, `←`/`→` moves between answers (`Ctrl+N` loads the next answers page) —
then `Enter` on an answer opens its comments — root comments as floors, replies
indented underneath, long threads completed by paging.

Zhihu is on for fresh installs (`enabled = ["rss", "hn", "zhihu"]` in `sources.toml`); see the
upgrade note above if your file predates it.

#### Cookie login (Ctrl+U)

Only the recommend feed, the follow feed and the answer-comments layer ask for a cookie — everything above works
without one. To log in:

1. Log in to `zhihu.com` in your browser.
2. Open DevTools (`F12`) → **Network** → reload the page.
3. Pick any request to `zhihu.com`, find the **Cookie** request header, and copy its whole value.
4. In readscope press `Ctrl+U`, paste, `Enter` to save (`Esc` cancels; input shows as dots and is
   never echoed). The CLI equivalent is `readscope zhihu login`.

The paste must contain `z_c0` (the session key) — a paste without it is refused, nothing is
written. The header is stored as `<config dir>/zhihu.cookies` with mode `0600`, and `readscope
doctor` reports its state. Saving is 保存即用: the next `F5` picks the new cookie up, no restart.

Cookies expire after days to weeks. When that happens the detail family answers 401/403 and the
UI says *login expired — `Ctrl+U` to re-login*; the recommend and follow feeds show the same guidance. Delete
the file and you are back in anonymous mode: hot board and daily digest keep working, the follow
feed asks for a login.

**Account risk, honestly:** readscope talks to zhihu's JSON APIs as a non-browser client with
your session cookie. Refreshes are manual (`F5`) — no polling, no background traffic — but zhihu
could still flag the pattern. Prefer a burner account while you evaluate it, paste cookies only
for accounts you accept that risk on, and run `readscope zhihu probe` (raw samples land under
`.scratch/`) if endpoints start answering oddly.

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
| Ctrl+N | Load more / page | Ctrl+Q | Quit |
| Ctrl+U | Zhihu login (paste cookie) | | |

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
