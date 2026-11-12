# 08: 推荐 feed — probe 实测 + 问题卡游标分页 + cookie 门禁

**What to build:** 边栏加 推荐 feed(cookie 必需),边栏序:热榜→推荐→关注→日报。分两步:

1. **probe 先行**:`zhihu probe` 加 recommend 端点(`www.zhihu.com/api/v3/feed/topstory/recommend`,Referer=`https://www.zhihu.com/`),用户 cookie 真网实测——匿名预期 401(同网关热榜 www 门)、cookie 形态、`paging.next` 游标形状、limit 参数无效性、**卡片构成与字段形态**(recommend 卡的 target 嵌套与 moments 是否同款:answer 卡的 `target.question.id`/`target.author.name`、question 卡、时钟字段名)。样本落盘 `.scratch/m4-zhihu/samples/`,数据面契约表按实测收口。
2. **feed 落地**:F5 从 recommend 首页拉取,过滤只出问题卡——answer 卡归并所属问题(id=`zh:question/<qid>`,元信息=回答作者进 author 列)、question 卡直入、article/pin/video/广告卡丢弃(article 卡 09 号票收编,本期丢弃)、同题去重。游标分页与关注流**完全同构、零新机制**:`paging.next`→`feed_fetch_state.cursor`,Ctrl+N 续翻(命中前进、到底不动、重启续翻),F5 重锚首页游标——直接复用 `refreshCursorFeed`/`moreCursor` 管线。无 cookie:preflight 零拨号报 `ErrLoginRequired`→状态栏 Ctrl+U 引导(04 号票语义)。

列表排序暂同关注流(ListSortTime,卡片自身时钟;时钟字段名 probe 复核,缺时钟则 FetchedAt)。

**Blocked by:** —(04/05 已落地,无前置)

**Status:** in-progress

- [x] probe:recommend 端点真网判定(匿名 401 预期、cookie 下 200、cursor 形状、卡片构成占比、时钟字段),样本落盘,契约表复核
- [ ] 边栏 推荐 feed,`zhihu.recommend` 双语标签(zh「推荐」/en「Recommend」),序:热榜→推荐→关注→日报;F5 首页入库
- [ ] 异构卡过滤:recommend fixture 混排 answer/question/article/pin/video/广告卡,断言只出问题卡、answer 归属正确、同题去重、article 本期丢弃
- [ ] 游标:Ctrl+N 沿 paging.next 翻页,命中才前进;到底不动;重启续翻不重拉已知页;F5 重锚(复用关注流管线,零新机制)
- [ ] 无 cookie:ErrLoginRequired 零拨号,状态栏 Ctrl+U 引导;单 feed 失败不影响其余源
- [ ] httptest 假 recommend 端点集成测试(多页游标 fixture),离线可跑

## Comments

- 2026-11-12 **probe 真网判定 GREEN**(samples/recommend.json):入口 `?desktop=true` 即可、无需 seed(session_token 由服务端在 paging.next 里发,2020 年资料的 session_token 入口顾虑解除);卡片字段与 moments 同款(question.id/author.name/created_time·updated_time),`questionCard` 复用成立;answer 卡 target.title 恒空(问题标题在 question.title);首页 cards=6(answer=5/article=1/dropped=0);游标第 2 页 200、after_id=11、与首页零重叠——游标深翻与 05 号票的 F5 重锚设计均成立;article 卡时钟字段名为 created/updated(unix 秒)、id 超 2^53(09 号票用 json.Number)。
- 2026-11-12 立票。背景:m4 spec 原把推荐 feed 列 Out of Scope(算法流),用户日常主刷推荐,边界推翻(spec 修订二)。决策:卡片范围问题卡+文章卡(09)、边栏序热榜→推荐→关注→日报、游标与关注流同构、cookie 门禁。
- 端点情报(立票时调研,probe 复核):`www.zhihu.com/api/v3/feed/topstory/recommend` 为知乎首页推荐流 XHR;limit 参数实测无效(zhihu-cli PR#5);FeedScope 有推荐 feed 但走常驻浏览器通道(本项目非目标),仅作端点佐证。
- 卡片映射:实测与 moments 同款,`questionCard` 参数化(feed key)复用。
