# readscope 项目方案

> 终端里的统一阅读器：RSS/HN 信息流 + 本地 EPUB 书库。
> Go + Bubble Tea 单二进制，中英双语，明暗双主题。
> 形态仿 [FeedScope](https://github.com/howardli/FeedScope)（Python/Textual），EPUB 部分参考 [goread](https://github.com/Ray-D-Song/goread)。

## 1. 定位与边界

**定位（已定）**：先自用，按开源标准起步——仓库从第一天起有 README、测试、CI、语义化版本；MVP 只服务自己的真实阅读流。

**目标**

- 信息流：RSS/Atom 订阅（含 OPML 导入/导出）+ Hacker News（公开 Algolia API，免登录）
- 书库：本地 EPUB 书架（书库目录扫描 + `add` 拷入）+ 全屏阅读器
- EPUB 标准版：目录导航、翻页滚动、进度记忆、书签、全文搜索、版面宽度调节；图片降级为 `[图片]` 链接标记
- 已读/收藏/进度全部持久化，缓存先行渲染，离线可读
- 首发中英双语 UI（TOML 语言包），内置 dark/light 双主题

**非目标（v1 明确不做）**

- 知乎/X/Reddit 登录态抓取——Go 没有现成的本机 Chrome 常驻通道，工程量以月计且随站点改版持续碎（这是 FeedScope 工程量的大头，不是它的形态）
- 终端内图像渲染（sixel/kitty 协议，兼容性碎片化）
- EPUB 高亮批注导出、mobi/azw3 格式
- 阅读进度云同步
- V2EX / Telegram 源（源接口预留，随时可加，不进 MVP）

## 2. 已定决策（ADR 一览）

| # | 决策点 | 结论 | 核心理由 |
|---|--------|------|----------|
| 1 | 项目定位 | 先自用，按开源标准起步 | 自用价值优先，不留开源返工债 |
| 2 | MVP 信息源 | RSS + HN + 本地 EPUB | 全部免登录公开接口，一个 HTTP 客户端通吃 |
| 3 | EPUB 边界 | 标准版（书架/TOC/翻页/进度/书签/搜索/宽度调节） | 阅读器期望底线；图像渲染和批注是独立大坑 |
| 4 | TUI 框架 | Bubble Tea + bubbles + lipgloss | Go TUI 事实标准，25000+ 应用，v2 生产验证 |
| 5 | 存储 | SQLite（modernc.org/sqlite 纯 Go）+ TOML 配置 | 无 cgo 交叉编译零痛苦；FTS5 直接吃下全文搜索 |
| 6 | 渲染管线 | 自建 DOM→行模型（x/net/html + chroma） | 中文折行、图片降级、内联样式全掌控；RSS 与 EPUB 共用 |
| 7 | 键盘模型 | 全非打印键（Tab/方向键/F 键），IME 安全 | FeedScope 为中文用户验证过的设计 |
| 8 | UI 语言 | 首发 zh-CN + en 双语，TOML 语言包 + 完整性测试 | 开源标准的第一道门面，成本低 |
| 9 | 项目名 | readscope | 致敬 FeedScope；Go/终端领域无重名（GitHub 仅一个异域 Python 数值包） |
| 10 | 书架来源 | 书库目录递归扫描 + `add` 子命令拷入 | 删文件即下架，心智模型最简单 |
| 11 | 界面布局 | 复刻 FeedScope：源栏+边栏+列表+分层详情；EPUB 独立全屏 | 已验证模型，窄终端不碎 |
| 12 | 里程碑 | 骨架→渲染管线 v1→RSS 日用→HN→EPUB 压轴 | 每阶段都有可用的东西，符合自用定位 |

## 3. 技术栈

| 层 | 选型 | 备注 |
|----|------|------|
| 语言 | Go 1.24+ | 本机 WSL 尚未安装：`sudo pacman -S go` |
| TUI | charmbracelet/bubbletea (+bubbles, lipgloss) | 锁定版本；v2 已在 Charm 自家产品生产运行 |
| 存储 | modernc.org/sqlite | 纯 Go 无 cgo；FTS5 编译开关确认可用 |
| 配置 | TOML（pelletier/go-toml/v2） | 人手可改，与 FeedScope 同形 |
| RSS/Atom | mmcdole/gofeed | 成熟，RSS2/Atom/JSON Feed 全覆盖 |
| HN | hn.algolia.com API | 与 FeedScope 同源，免 key |
| HTML 解析 | golang.org/x/net/html | 渲染管线入口 |
| EPUB 解析 | 先评估 billdaws/epub 与 ABFCode/Spine；不满足则自写薄层 | EPUB=zip+OPF+NCX/Nav，自写约一两天 |
| 代码高亮 | alecthomas/chroma | 管线 code block 集成 |
| 字宽/折行 | mattn/go-runewidth | CJK 双宽字符与禁则处理 |
| i18n | 自建 TOML 语言包 | 仿 FeedScope：一语言一文件，测试强制键树一致 |
| CLI 子命令 | 手写 flag dispatch（doctor/rss/add…） | 零额外依赖，膨胀了再上 cobra（M0 定） |
| 发布 | goreleaser + GitHub Actions | Linux/macOS/Windows 三平台 |

## 4. 架构与包结构

分层借鉴 FeedScope 的 core/sources/ui + goread 的 parser/reader：

```
readscope/
├── cmd/readscope/          # main + 子命令 (tui默认/doctor/rss/add/render调试)
├── internal/
│   ├── core/               # 领域模型: Item, Feed, Book, Progress, Bookmark
│   ├── source/             # Source 接口 + 注册表（加源=加一个包）
│   │   ├── rss/            # gofeed 封装: fetch/parse/条件请求
│   │   └── hn/             # Algolia: 列表/详情/评论树
│   ├── library/            # 书库: 目录扫描/add 拷入/元数据缓存
│   ├── epub/               # EPUB 解析: spine/toc/metadata
│   ├── render/             # ★ 渲染管线（核心资产）
│   │   ├── dom.go          #   HTML walker → Block 序列（白名单标签映射）
│   │   ├── block.go        #   Block/Line/Style 行模型
│   │   ├── wrap.go         #   折行: rune 级 + CJK 禁则（行首行尾标点）
│   │   └── code.go         #   chroma 代码高亮
│   ├── store/              # SQLite: schema/migrations/DAO/FTS5
│   ├── i18n/               # TOML 语言包加载 + 键树校验
│   ├── theme/              # lipgloss 样式集 (dark/light)
│   └── ui/                 # Bubble Tea 层
│       ├── app.go          #   顶层 model: 源栏/边栏/列表 三区焦点导航
│       ├── list.go         #   列表屏（●未读点/已读变暗/元信息列）
│       ├── detail.go       #   分层详情（RSS全文 / HN评论楼层）
│       ├── reader.go       #   EPUB 全屏阅读（左TOC+右viewport）
│       ├── bookshelf.go    #   书架屏
│       ├── dialogs.go      #   源管理/订阅管理/输入/确认
│       └── keys.go         #   全非打印键 keymap + 误触提示
├── configs/i18n/           # zh-CN.toml, en.toml
├── docs/                   # PLAN.md（本文档）
└── .github/workflows/      # ci.yml, release.yml
```

**Source 接口（最深的一道缝）**：

```go
type Source interface {
    ID() string
    Feeds() []Feed                                   // 边栏的 feed 列表
    Fetch(ctx context.Context, f Feed) ([]Item, error) // 列表层
}
// 可选扩展：HN 实现详情层（评论树），RSS 的正文随缓存走
type DetailSource interface {
    Detail(ctx context.Context, it Item) (Content, error)
}
```

## 5. 数据模型

SQLite（`~/.local/share/readscope/readscope.db`，遵循 XDG）：

```sql
items(id, source, feed, title, url, author, published_at, fetched_at, body)
  -- body 存快照：离线可读、收藏不失效（FeedScope favorites 同思路）
read_state(item_id PK, read_at, starred_at, star_snapshot)
  -- star_snapshot 存收藏时刻的完整快照，源文章消失仍可读
comments(id, item_id, parent_id, author, text, score)   -- HN 评论树缓存
books(id, path, title, author, language, added_at)
progress(book_id PK, spine_index, block_offset, pct, updated_at)
bookmarks(book_id, spine_index, block_offset, note, created_at)
fts USING fts5(title, body, item_id UNINDEXED, book_id UNINDEXED)
  -- 文章与书籍章节统一进全文索引
```

TOML 配置（`~/.config/readscope/`）：

```toml
# config.toml
language = "zh-CN"        # 首启询问，locale 预选
theme = "dark"
book_dir = "~/Books"
text_width = 0            # 0 = 随终端

# sources.toml
enabled = ["rss", "hn"]   # 顺序 = 源栏顺序
[rss.feeds]
ruanyq = { label = "阮一峰周刊", url = "https://www.ruanyifeng.com/blog/atom.xml" }
```

`readscope rss import subs.opml` / `export`：与主流阅读器互迁无锁定。

## 6. 渲染管线（核心技术设计）

RSS 全文与 EPUB 章节共用同一条管线，本项目最重的自建组件：

```
XHTML/HTML ──x/net/html──▶ DOM ──walker──▶ Block 流 ──布局──▶ styled Lines ──▶ viewport
```

```go
type Block interface{ block() }
// Heading{Level,Text}  Para{Spans}  Quote{[]Block}  Code{Lang,Src}
// Image{Alt,Ref}       ListItem{Marker,Blocks}  Rule
// 链接收集编号：正文内 [1]，文末列 URL；图片渲染为 [图片: alt] 标记，
// Enter 用系统默认看图程序打开（goread 同款降级）
```

要点：

- **CJK 折行**：按 rune 折行（不是字节），go-runewidth 算宽，禁则处理（行尾不开 `（「`、行首不放 `。，」`）
- **EPUB 增厚层**（M4）：spine 章节流 → 全书 Block 流分页模型；进度 = (spine_index, block_offset)，与字号/终端宽无关，换机换终端不丢；TOC 从 NCX/Nav 解析
- **golden file 测试**是管线质量底线：样例 HTML/真实中文 EPUB 章节 → 期望行模型快照，防回归
- 调试出口：`readscope render <url|file>` 子命令直接在终端吐排版结果，管线独立于 TUI 可测

## 7. UI 与键位

布局（复刻 FeedScope）：

```
┌─ 源栏: [书架] [RSS] [HN] ──────────────── Tab 切换 ─┐
│ 边栏     │  列表（●未读点 · 标题 · 元信息列）        │
│ (feeds)  │  已读标题变暗；F4 只看未读                 │
│ 未读计数  │  Enter → 详情层（覆盖列表区）             │
├──────────┴─ 状态栏: N 条 · M 新 │ 当前 feed │ 提示 ─┤
└─────────────────────────────────────────────────────┘
EPUB 阅读模式 = 独立全屏：左 TOC 侧栏 + 右正文 viewport，Esc 回书架
```

键位（全非打印键，中文 IME 激活不冲突；误触打印键 → 状态栏提示）：

| 键 | 作用 | 键 | 作用 |
|----|------|----|------|
| Tab / Shift+Tab | 切源 | ←→↑↓ | 区域导航/滚动/翻页 |
| Enter | 开详情/进楼层 | Esc | 退一层 |
| F1 | 键位帮助 | F2 | 源/订阅管理 |
| F3 | 收藏列表 | F4 | 未读过滤 |
| F5 | 刷新 | Ctrl+N | 加载更多 |
| Ctrl+O | 浏览器打开 | Ctrl+Y | 复制链接 |
| Ctrl+R | 切已读/未读 | Ctrl+A | 全部已读(确认) |
| Ctrl+F | 收藏/取消 | Ctrl+T | 切主题 |
| 阅读模式 | Ctrl+B 书签 / Ctrl+S 搜索 / +/- 宽度 | Ctrl+Q | 退出 |

（阅读模式内 Ctrl+F 不再收藏而是搜索——书籍无收藏语义，实现期定稿进 F1 帮助。）

## 8. 里程碑（每步完成即可用）

| 阶段 | 内容 | 验收标准 | 估时 |
|------|------|----------|------|
| **M0 骨架** | 建仓/go mod/目录/CI/i18n 框架+双语包/主题/SQLite schema+migrations | `readscope` 出空界面；`readscope doctor` 自检过；CI 绿 | 0.5 周 |
| **M1 渲染管线 v1** | DOM→Block→折行→高亮；golden 测试；`render` 子命令 | 中英文混排样例排版正确（标题/引用/代码/链接编号/禁则） | 1 周 |
| **M2 RSS 日用** | gofeed 拉取+ETag 条件请求+缓存；列表/详情/已读/收藏/F4/Ctrl+A；OPML 导入导出 | 日常订阅可刷可读，断网重启列表即现、正文可读 | 1–1.5 周 |
| **M3 HN** | Algolia 列表+详情+评论树（楼层制，深层两层扁平化+@parent，FeedScope 同规则） | 无账号完整读帖跟评 | 0.5–1 周 |
| **M4 EPUB 书库+阅读器** | 解析(评估 billdaws/Spine→或自写)/书架/全屏阅读/TOC/进度续读/书签/宽度调节/FTS5 搜索 | 一本中文技术书完整读完，重启精确续读，全书可搜 | 2–3 周 |
| **M5 打磨发布** | 主题切换/帮助屏/doctor 完善/goreleaser 三平台 | v0.1.0 tag，`go install` 与二进制均可装 | 1 周 |

合计约 **6–8 周业余时间**。风险最大的 M1/M4 各自带测试底线和独立调试出口，超支不殃及全局。

## 9. 风险与对策

| 风险 | 对策 |
|------|------|
| Bubble Tea v2 仍在演进 | go.mod 锁版本；v2 已跑在 Charm 生产产品上，必要时回 v1 稳定 API |
| 渲染管线工作量超预期 | M1 独立成阶段 + golden 测试；goread pkg/parser 是现成参考（MIT，可对照实现） |
| CJK 折行/禁则细节坑多 | go-runewidth 打底；第一批 golden 用例就用真实中文书籍章节 |
| 现实世界 EPUB 脏（DRM/非标/损毁） | Spine 的容错思路：partial data + warnings 照常开书；DRM 明确报「不支持」 |
| modernc/sqlite 体积（约 +10MB） | 换 bbolt 则失去 FTS5，不值；接受体积 |
| readscope 重名 | GitHub 仅 `ahb-sjsu/readscope`（Python 数值计算，异域）；PyPI 被占但与我们无交集；若在意，建仓前改名的最后窗口就是现在 |
| 单人维护带宽 | 里程碑小步快跑，每步收口；源接口让「加源」永远是增量 |

## 10. 先例索引

- **FeedScope**：布局/键位/已读收藏模型/i18n TOML 设计/one-package-per-source 架构的蓝本
- **goread**（tview，230★）：EPUB 解析、HTML→终端样式行、TOC/搜索/宽度调节的实现参考
- **goreader**（tcell，101★）：更老的单本阅读器，翻页模型参考
- billdaws/epub（零依赖读写）、ABFCode/Spine（容错解析）：EPUB 库候选
- Charm 生态：bubbletea/bubbles/lipgloss 文档与 examples

## 11. 下一步

1. **前置**：WSL 装 Go（需你输密码）：`sudo pacman -S go`
2. 确认本方案 → 建仓 scaffold（git init、go mod、目录骨架、CI、i18n 双语包、SQLite migration 框架）
3. M0 动工，从 `readscope doctor` 开始第一条 tracer bullet
