# readscope

> 终端里的统一阅读器 —— RSS 订阅、Hacker News 和本地 EPUB 书库，一个终端全装下。

**语言**: [English](README.md) · 中文

状态：**0.x，开发中。** readscope 是 Go 编写的终端优先阅读器：信息流（RSS/Atom 订阅 + Hacker News）与本地 EPUB 书架共用一套纯键盘、对中文输入法友好的界面。形态仿 [FeedScope](https://github.com/howardli/FeedScope)。

## v0.1 规划特性

- **信息流**：RSS/Atom 订阅，OPML 导入/导出；Hacker News 走公开 Algolia API——免登录、免 key
- **EPUB 书架**：书库目录扫描，`add` 拷入书籍；独立全屏阅读模式，含目录导航、书签、全文搜索
- **状态不丢**：已读标记、收藏（完整快照）、阅读进度存 SQLite；缓存先行渲染，离线可读
- **中文友好的渲染**：RSS 正文与 EPUB 章节共用一条 HTML→终端排版管线，中文折行（禁则处理）、代码高亮
- **输入法安全键位**：全部非打印键（Tab / 方向键 / F 键），中文 IME 激活时照常工作
- **中英双语 UI** + 明暗双主题

v0 不做：登录态抓取源（知乎/X/Reddit）、终端内图像渲染、批注导出、mobi。

## 安装

尚未发布——里程碑推进中。源码构建（Go 1.24+）：

```bash
git clone https://github.com/smy-101/readscope.git
cd readscope
go build ./cmd/readscope
```

RSS 日用（M2）与 Hacker News（M3）已经可用；EPUB 书库（M4）随后。

## 使用

```bash
readscope                 # 启动 TUI（默认）
readscope doctor          # 环境自检，含逐订阅可达性检查
readscope rss refresh     # 拉取全部订阅进离线缓存
readscope rss import F    # 将 OPML 订阅合并进 sources.toml
readscope rss export F    # 导出订阅为标准 OPML 2.0
readscope render <文件|URL>  # 将 HTML 排版为终端文本（管线调试）
readscope version         # 打印版本
```

### RSS

订阅写在 `sources.toml`，与 `config.toml` 同目录（`readscope doctor` 会打印两个路径）：

```toml
enabled = ["rss", "hn"]

[rss.feeds]
ruanyifeng = { label = "阮一峰周刊", url = "https://www.ruanyifeng.com/blog/atom.xml" }
hn = { label = "Hacker News", url = "https://hnrss.org/frontpage" }
```

map 的键是稳定 slug（用于条目 id 与边栏）；`label` 可省，回退为键名。刷新走条件请求
（ETag / If-Modified-Since），未变的订阅只花一个 304、跳过解析；条目快照进 SQLite——
列表从缓存秒出，断网也能读全文。TUI 里按 `F5` 后台刷新。

与其他阅读器互迁走 OPML：`readscope rss import subs.opml` 合并（已有订阅不动），
`readscope rss export subs.opml` 导出标准 OPML 2.0。

> 从旧版升级？`sources.toml` 不会被自动改写——手动在 `enabled` 里加一行 `"hn"`
> 即可开启下面的 Hacker News 源。

### Hacker News

Tab 切到 Hacker News，边栏固定四类 feed——热门 / 最新 / Ask HN / Show HN——数据来自
[Algolia HN Search](https://hn.algolia.com) 公共索引（免账号、免 key）。`F5` 把当前
feed 拉进与 RSS 同一套离线缓存：帖子快照进 SQLite，● 未读点 / 已读 / 收藏机制完全
一致，断网照样可读。列表里 `Ctrl+N` 追加下一页（不重复、已读保持）；帖子内
`Ctrl+N` 在楼层/回复间内存翻页。帖子阅读分三层——正文、楼层（每条顶层评论各
一楼）、回复树——深层回复压成两层、跨层带 `@parent` 前缀，窄终端也看得清谁
回复谁。HN 刷新恒为无条件重拉（索引无条件请求语义）。链接帖的正文
就是一行原文链接（`Ctrl+O` 直接打开）；文本帖（Ask/Show）的正文走与 RSS 同一条
渲染管线。注意：Algolia 索引滞后站点几分钟，排序按 points/时间而非 HN 官方排名
——readscope 不承诺官方排序。

`readscope doctor` 会用同一个 User-Agent 逐个试连订阅，报告 ✓ 可达（带耗时）、
! 慢（超过 3 秒，警告）或 ✗ 失败（退出码 1）——一眼分清是源挂了还是环境坏了。

### 键位

全部非打印键，中文输入法激活时照常工作：

| 键 | 作用 | 键 | 作用 |
|----|------|----|------|
| Tab / Shift+Tab | 切换源 | F5 | 刷新 |
| ← → ↑ ↓ | 移动 / 滚动 | Ctrl+R | 切换已读/未读 |
| Enter | 打开 / 进入 | Ctrl+A | 全部已读（需确认） |
| Esc | 退回一层 | Ctrl+F | 收藏/取消 |
| F1 | 键位帮助 | Ctrl+O | 浏览器打开 |
| F3 | 收藏列表 | Ctrl+Y | 复制链接 |
| F4 | 只看未读 | Ctrl+T | 切换主题 |
| Ctrl+N | 加载更多/翻页 | Ctrl+Q | 退出 |

（F2，TUI 内订阅管理，后续里程碑提供。）

## 开发

```bash
go build ./...       # 构建
go test ./...        # 测试
go vet ./...
```

设计文档：[docs/PLAN.md](docs/PLAN.md)。

## 许可

[MIT](LICENSE)
