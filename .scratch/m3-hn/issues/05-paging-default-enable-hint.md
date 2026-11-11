# 05: 收尾机制 — 列表翻页 + 默认启用 + 空白提示

**What to build:** 三件收尾机制:① HN 列表 Ctrl+N 拉 Algolia 下一页追加进缓存(与 F5 刷首页互补);② 新装默认启用 `["rss","hn"]` 零配置可用,存量 sources.toml 不自动改写(README 记手动加法);③ Tab 切到未注册源(书架)时状态栏显示"后续里程碑提供",消除谜之空白(本次 M3 的起因)。

**Blocked by:** 01

**Status:** ready-for-agent

- [x] Ctrl+N 追加下一页:不产生重复行、已读状态保持;状态栏反馈加载/失败/到底
- [x] F5 仍刷第一页;Ctrl+N 追加与 F5 刷新语义互不破坏 → review 转入(spec 决策:F5 = 重拉**当前 feed** 第一页):当前 F5 走 refresh.Source 刷全源四 feed(源无关机制,可见需求成立,代价 3 个多余 GET);本票做页感知拉取时一并改作 feed 级刷新
- [x] 全新环境首启默认 enabled = `["rss","hn"]`,零配置可见 HN(01 已交付,commit 5784259)
- [x] 已存在的 sources.toml 不被自动改写;README(zh-CN / en)记录手动启用与 HN 用法(01 已交付)
- [x] 切到未注册源(书架)状态栏显示"后续里程碑提供";已注册源不受影响(01 已交付,TestTabToUnimplementedSourceHintsLater)

## Comments

**2025-12 实现决策**:

- 页感知拉取走第三道可选扩展缝 `source.PagedSource`(`FetchPage(ctx, feed, page)`,与 ConditionalSource/DetailSource 同款 probe-don't-register 模式):hn 实现——page 0 不带参数(Algolia 默认),后续页 `&page=N`;空 hits 即索引到底(HTTP 200 实测定形),不依赖 nbPages。rss 不实现:列表是全量快照,第一页之后无页可翻
- 页深(已连续拉取多少页)持久化在 `feed_fetch_state` 新列 `pages_fetched`(migration 007):(source, feed) 本就一行,validators 与页深互不clobber——重启后续翻不重拉已知页。按行数算页深不可靠:HN 同一故事在 Front/New 间换舵(re-homing),行数含迁入行会过冲
- `refresh.More` 契约:拉存储页深的那一页 → SaveFeedItems upsert(去重、read_state 不碰)→ 仅当页有命中时页深+1;空页 = end=true 且页深不动(索引增长后同页重试)。F5(`refresh.Feed`)成功后把页深保底 1——Ctrl+N 从 page 1 续走,不重拉 page 0;F5 不抬页深上限(不缩不涨)
- F5 feed 级回退规则:选中真 feed → `refresh.Feed`(rss 仍走条件请求路径,ETag 照常);favorites 视图/无选中 feed → 回退全源 `refresh.Source`(无当前 feed 可言,保持 M2 行为)。CLI `rss refresh` 保持全源不动
- UI 反馈:非分页源/favorites/无选中 feed 的 Ctrl+N 统一 status.more_end("没有更多了"),拒绝谜之空操作;moreDoneMsg 带源/feed 键做陈旧路由(用户已切走则只入缓存不出声);加载中状态栏前缀 loading_more。新 i18n 键:loading_more / more_done / more_none / more_end,双语键树测试守住
- 已读保持零新代码:SaveFeedItems 的 upsert 从不碰 read_state(M2 既定),paging 只是复用;测试在 UI 缝钉住(re-served 行不重不丢已读)

**2025-12 05 号票评审**(两轴并行,均 OK with notes):

- 修复:README.md 键位表 Ctrl+Q 重复行(英文表新增未删占位行,zh-CN 对了);Feed/More 的 feed-key 查找合一为 findFeed;ctrl+n 分支去掉不可达的 source.New 探针(未注册源无 feed,空键分支已拦);paging_test 的 rssXML 双定义合一
- 采纳:GLOSSARY 补"分页源/页深"两词条
- 拒绝(仅记录):fetchstate.go 双因变化(validators+页深)——行共存是票内已定权衡,第三种因出现时再拆
