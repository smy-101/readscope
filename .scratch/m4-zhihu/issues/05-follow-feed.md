# 05: 关注 feed — 问题卡过滤 + 游标分页

**What to build:** 边栏加 关注 feed(cookie 必需)。F5 从 `www.zhihu.com/api/v3/moments?limit=10&desktop=true` 拉(Referer=follow 页,共享 transport 注入 cookie);条目过滤只留问题卡:answer 卡归并到所属问题(id=`zh:question/<qid>`,元信息=回答作者名进 author 列),question 卡直入,article/pin/video/feed_group/广告卡丢弃;同题去重。游标分页:`paging.next` 存入 `feed_fetch_state` 新 cursor 列(下一号 migration),PagedSource 扩展为游标变体——Ctrl+N 拉当前游标页,命中才前进游标,空页=到底且游标不动;重启从存储游标续翻。无 cookie 时走 04 号票的登录引导。

**Blocked by:** 04

**Status:** done (2026-11-12)

- [x] 边栏 关注 feed,标签双语;F5 首页入库,元信息列=作者名
- [x] 异构卡过滤:moments fixture 混排 answer/question/article/pin/video/广告卡,断言只出问题卡、answer 卡归属正确、同题去重
- [x] 游标分页:Ctrl+N 沿 paging.next 翻页,命中才前进;空页=到底;重启续翻不重拉已知页
- [x] cursor 列 migration 上线,页深语义文档同步(分页源/页深词条补一句游标变体)
- [x] cookie 缺失/失效在关注 feed 的呈现符合 04 号票语义;单 feed 失败不影响其余源
- [x] httptest 假 moments 端点(多页游标 fixture)集成测试,离线可跑

## Comments

- 2026-11-12 实现完成。缝:`source.CursorPagedSource`(`CursorPaged(f)` 按 feed 判形态 + `FetchCursorPage(ctx, f, cursor) (items, next, err)`),与 PagedSource 同款 probe-don't-register;调用方先探游标再探页码,zhihu 只实现游标变体——热榜仍走 ErrNotPaged → "没有更多了"(TestCtrlNOnZhihuHotBoardSaysNoMore 钉住)。
- 存量:migration 008 给 `feed_fetch_state` 加 `cursor` 列(下一页的 paging.next,""=冷启动);`LoadCursor/SaveCursor` 与 validators/pages_fetched 同行三列互不干扰(TestCursorRoundtrip)。zhihu 不实现 PagedSource,关注 feed 的 pages_fetched 恒 0,游标即位置。
- 刷新链:refreshOneFeed 对游标 feed 先叉到 `refreshCursorFeed`(F5 = 首页入库 + 游标重锚);`More` 叉到 `moreCursor`(Ctrl+N = 拉存储游标页)。UI 零改动——moreDoneMsg/refreshStatus/引导映射链全是既有管线。
- **到底信号取服务端,不取条目数**(对票据"空页=到底"的细化):到底 = 响应不再带 `paging.next`(is_end)。被过滤空页(整页 pin/article)不算到底、游标照常前进——否则一次想法连载就永久卡死 Ctrl+N 在那页;该页报"没有新条目"(more_none)。到后续 Ctrl+N 重发同一游标、重报到底,不回卷到首页。
- **F5 重锚游标**(与页深"F5 只保底不抬高"的有意差异,GLOSSARY 页深已记):不透明游标无法与位移后的流对齐,故每次 F5 把游标重写为首页 next(next 为空则保留旧位,避免"永远重拉首页"循环);顺带自愈过期游标。真网若证实游标会过期且需要保留深读位置,再翻回"只保底"即可,单点在 refreshCursorFeed。
- 问题卡映射:answer 卡 → 所属问题(qid 取 `target.question.id`,json.Number 大整数精确;作者列=回答者),question 卡直入(作者列=提问者,无则空),其余卡型丢弃;同页同题首批保留。时间戳取 updated_time,缺则 created_time,再缺则 PublishedAt 置空——关注流按卡片自身时钟排序,否则深页(更晚拉取)会浮到浅页之上;真网字段名由 07 号票 smoke 复核。
- 04 号票转交收口:TestFollowFeedF5WithoutCookieShowsLoginGuidance 用真 refresh.Feed 走 无 cookie → 状态栏引导 全链(假后端对任何拨号回 500,顺带钉住 preflight 零拨号)。
- 集成测试 TestFollowFeedCursorWalk:四页游标走查(首页/重复行页/pin 空页/到底页)+ 到底重报 + F5 重锚,并以拨号序列断言"重启续翻不重拉已知页"(每次调用都重建 source,即重启语义)。
- 边栏/标签/元信息:zhihu.follow 双语标签;列表元信息列关注行=作者名(热榜行仍为热度,无热度无作者则空,见 ui.zhihuMeta)。

## Review

(pending — 两轴评审随收口提交)
