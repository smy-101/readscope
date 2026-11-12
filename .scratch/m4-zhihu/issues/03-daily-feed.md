# 03: 知乎日报 feed — 列表 + 全文详情 + 离线

**What to build:** 边栏加 知乎日报 feed(免登录)。F5 从 `news-at.zhihu.com/api/4/news/latest` 拉当日条目(`zh:daily/<storyId>`),元信息列 = hint("作者 · 2 分钟阅读");Enter 走 DetailSource:拉 `api/4/story/{id}` 的 body(HTML)存入 body 快照,经 zhihu 包 DOM 清洗后进 M1 渲染管线;二次打开渲染缓存零请求,离线可读。与 RSS 全文同形(正文随缓存走)。日报无评论层,Enter 已在全文层。

**Blocked by:** 02

**Status:** done (2026-11-12)

- [x] 边栏 知乎日报 feed,标签双语;F5 拉当日条目入库,id 确定性
- [x] Enter 打开:story body 拉取→DOM 清洗→渲染管线排版(标题/正文/图片降级标记)
- [x] 正文进 body 快照:离线重启后已开条目可读,未开条目离线时明确提示
- [x] 元信息列 = hint;缺 hint/图片的条目不崩
- [x] httptest 假日报端点(列表 + story fixture)集成测试,离线可跑
- [x] 清洗 golden 样例过 M1 管线快照测试(日报 HTML 子集)

## Comments
- 02 评审转交:rowMeta 的 per-source if 级联到第二臂(hn/zhihu);日报 hint 元信息是首例源内按 feed 分流 —— 落地时考虑表驱动/per-feed 格式化,别加第三臂 if。
- **转交已落地**:hint 存新列 `items.meta`(migration 009——源把 hint 作为不可拆分的原子串服务,author 列不重载);`ui.zhihuMeta` 改为按 feed 路由(热榜=热度快照、关注=author、日报=meta),rowMeta 本体未加臂。meta 列语义:仅存源无法分解的原子元信息, hn/热榜/关注等由语义字段组合的继续用字段。
- 新增 `source.BodySource` 扩展(probe-don't-register 第 5 员):`BodyOnDemand(it)` 探针 + `FetchBody(ctx,it)`;实现落在 zhihu 包(仅 `zh:daily/` 前缀)。拉取+落库走 `refresh.Body`→`store.SaveBody`(body+FTS 同步,fetched_at 不动)。按需正文词条已进 GLOSSARY。
- **转交 06**:zhihu 实现 DetailSource 后,`ui.openDetail` 的 hasDetail 探测需按 item 分流——问题卡走评论层,日报条目绝不能出现评论 footer/floors(现在 zhihu 无 DetailSource,全源 hasDetail=false 恰好安全;06 落地时这个隐蔽耦合会浮出)。CleanHTML(clean.go)已就位,问题详情/回答 HTML 直接复用;golden 机制在 zhihu/testdata 下(-update 同 render)。
- `SaveFeedItems` 新规则:**空正文不覆盖已缓存正文**(items 行与 FTS 同步生效)——否则日报 F5 会洗掉已开条目的正文;非空正文照常覆盖(真正改稿)。rss/hn 行为不变(它们列表层总带正文)。
- 离线语义:首开失败 = 状态栏「正文拉取失败:…」+ no_body 占位(明确提示);已开条目二次打开渲染快照零请求(UI 测试以 story door 命中计数钉死);F5 后已缓存正文存活(store 测试钉死)。
- 测试基建:`fakeZhihu` 增 dailyLatestOverride/dailyStoryOverride 两腿;`brokenServer` 助手归 daily_test。
