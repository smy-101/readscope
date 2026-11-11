# 05: 关注 feed — 问题卡过滤 + 游标分页

**What to build:** 边栏加 关注 feed(cookie 必需)。F5 从 `www.zhihu.com/api/v3/moments?limit=10&desktop=true` 拉(Referer=follow 页,共享 transport 注入 cookie);条目过滤只留问题卡:answer 卡归并到所属问题(id=`zh:question/<qid>`,元信息=回答作者名进 author 列),question 卡直入,article/pin/video/feed_group/广告卡丢弃;同题去重。游标分页:`paging.next` 存入 `feed_fetch_state` 新 cursor 列(下一号 migration),PagedSource 扩展为游标变体——Ctrl+N 拉当前游标页,命中才前进游标,空页=到底且游标不动;重启从存储游标续翻。无 cookie 时走 04 号票的登录引导。

**Blocked by:** 04

**Status:** ready-for-agent

- [ ] 边栏 关注 feed,标签双语;F5 首页入库,元信息列=作者名
- [ ] 异构卡过滤:moments fixture 混排 answer/question/article/pin/video/广告卡,断言只出问题卡、answer 卡归属正确、同题去重
- [ ] 游标分页:Ctrl+N 沿 paging.next 翻页,命中才前进;空页=到底;重启续翻不重拉已知页
- [ ] cursor 列 migration 上线,页深语义文档同步(分页源/页深词条补一句游标变体)
- [ ] cookie 缺失/失效在关注 feed 的呈现符合 04 号票语义;单 feed 失败不影响其余源
- [ ] httptest 假 moments 端点(多页游标 fixture)集成测试,离线可跑
