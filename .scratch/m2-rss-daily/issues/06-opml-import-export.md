# 06: OPML 导入/导出

**What to build:** 用户从主流阅读器(NetNewsWire、Inoreader 等)导出 OPML,`readscope rss import subs.opml` 把订阅合并进 `sources.toml`(已有订阅不重复);`readscope rss export subs.opml` 导出可被其他阅读器导入的 OPML。与主流工具互迁无锁定。与 TUI 票并行。

**Blocked by:** 01(`readscope rss refresh` — 订阅拉取入缓存)

**Status:** ready-for-agent

- [ ] 导入:解析真实世界 OPML(folder 分组、xmlUrl/title 属性),合并写入 sources.toml,报告 新增/跳过
- [ ] 导出:生成标准 OPML(outline 层级),主流阅读器可导入
- [ ] 往返测试:export → import 后订阅集合一致
- [ ] 容错:脏 OPML(缺 title、非标嵌套)不崩,能导入多少导多少并警告
- [ ] 双语 CLI 输出(走 i18n 语言包)
