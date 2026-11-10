# M2: RSS 日用 — Spec

上游方案:`docs/PLAN.md` §8 里程碑 M2,术语与决策以 PLAN 为准(ADR #2 RSS+HN+EPUB、#5 SQLite+FTS5、#6 渲染管线共用、#10/#11 布局与来源、#12 里程碑)。

## 目标(验收标准,来自 PLAN)

日常订阅可刷可读;断网重启 readscope,列表即现、正文可读。

范围:

- gofeed 拉取 + ETag 条件请求 + 缓存(items 表快照,离线可读)
- TUI:边栏真实 feeds(未读计数)、列表屏(●未读/已读变暗)、详情层(M1 渲染管线)、已读/收藏/F4 未读过滤/Ctrl+A 全部已读/F3 收藏列表
- `readscope rss import/export`:OPML 互迁

## 明确不在 M2 内

- F2 订阅管理对话框(键位先留提示,M5 打磨或 backlog)
- Ctrl+N 加载更多/分页(数据量到需要时再做)
- HN 源、EPUB(M3/M4)
