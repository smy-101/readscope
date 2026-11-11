# 06: OPML 导入/导出

**What to build:** 用户从主流阅读器(NetNewsWire、Inoreader 等)导出 OPML,`readscope rss import subs.opml` 把订阅合并进 `sources.toml`(已有订阅不重复);`readscope rss export subs.opml` 导出可被其他阅读器导入的 OPML。与主流工具互迁无锁定。与 TUI 票并行。

**Blocked by:** 01(`readscope rss refresh` — 订阅拉取入缓存)

**Status:** ready-for-human

- [x] 导入:解析真实世界 OPML(folder 分组、xmlUrl/title 属性),合并写入 sources.toml,报告 新增/跳过
- [x] 导出:生成标准 OPML(outline 层级),主流阅读器可导入
- [x] 往返测试:export → import 后订阅集合一致
- [x] 容错:脏 OPML(缺 title、非标嵌套)不崩,能导入多少导多少并警告
- [x] 双语 CLI 输出(走 i18n 语言包)

## Comments

- 落点 `internal/opml`(Parse/Import/Encode/Report,与 refresh 包同构:数据结果 + 本地化报告),CLI 薄接线在 `cmd/readscope/rss.go`。
- 键派生规则:ASCII 标题 slug 化("Hacker News" → `hacker-news`);CJK 标题保留为 label、以 URL host 为稳定键;冲突加 `-2` 后缀;缺标题以 host 命名并警告。
- 导入隐含启用 rss 源(enabled 缺 rss 时追加);已有订阅永不改动,URL 去重(配置内 + 文档内)。
- 导出为标准 OPML 2.0,按键序稳定输出(无 dateCreated,字节级稳定);无 label 时 text 回退为键。
- refresh 保持严格配置错误退出;import/export 用宽松目录加载(config.toml 坏不阻塞维护命令)。

**评审记录(2025-11-11, 双轴 review):** 无阻塞项;两条 P2 记为决策:
- 语法级脏 XML(非枚举的容错场景)整篇拒绝并退出 1 —— "能导入多少导多少"限定于票面枚举的内容级脏(缺 title、非标嵌套);容忍语法损坏的正则扫描属投机机制,不做。
- 导出为扁平 outline:PLAN §5 的 sources.toml 无 folder 概念,层级无处安放;主流阅读器真机导入冒烟并入 07 号验收票执行。
- 评审顺手项:rss.go 三处 LoadSources 错误块提取为 loadSources() 助手。
