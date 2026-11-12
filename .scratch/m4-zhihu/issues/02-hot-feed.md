# 02: 源包骨架 + 热榜 feed — 注册→拉取→入库→列表

**What to build:** `internal/source/zhihu` 包落地,id `"zhihu"`,按"加源 = 加一个包"注册进源注册表;边栏固定 热榜 feed(Following/日报 在 03/05 票进);F5 从 `api.zhihu.com/topstory/hot-list` 拉全量(≤30 条,免登录,仅桌面 UA),问题卡作为离线快照写入 items(`zh:question/<qid>`,qid 从 target.url 提取),列表立即显示;元信息列显示热度(detail_text 折算进 points 热度快照列,呈现还原 "2045 万热度");无翻页(非 PagedSource,Ctrl+N 提示"没有更多了")。● 未读点/Ctrl+R/F4/Ctrl+A/F3 沿用源无关机制。DefaultSources 改 `["rss","hn","zhihu"]`,已存在的 sources.toml 不自动改写(README 记手动加法)。

**Blocked by:** 01 (数据面判定至少热榜 GREEN)

**Status:** done (2026-11-12)

- [x] Tab 切到知乎:边栏出现热榜 feed,标签双语,i18n 键树一致(ui/zhihu_test + i18n parity 测试;en "Hot List" / zh-CN "热榜")
- [x] F5 拉热榜:问题卡入库为快照;id 确定性,重复刷新不产生重复行、不触碰已读状态(refresh tracer:二刷 0 新行、read 存活、热度快照原地更新)
- [x] qid 从 target.url 提取;fixture 含超 2^53 裸整数 id,不丢精度(fixture 18984420158198241699;真网 smoke 实捧 2053053168834064899 等 19 位 qid)
- [x] 元信息列 "N 万热度"(points 快照折算/还原,"2045 万热度"↔20450000);非 question 卡与缺 title 的行跳过不崩,错误信息带 feed 名(fetch/parse 错误均命名 hot)
- [x] ● / Ctrl+R 对知乎条目与 RSS/HN 行为一致(按键泵测试;F4/Ctrl+A/F3 走同一套源无关机制,既有测试覆盖)
- [x] httptest 假热榜端点(fixture JSON)集成测试,离线可跑(复用 probe_test 的 fakeZhihu,新增 hotOverride)
- [x] DefaultSources 含 zhihu;无 cookie 场景热榜照常(免登录;真网 smoke 无 cookie 头世验证 30 行)

## Comments

- 2026-11-12 实现完成。真网 smoke(生产路径 refresh.Feed→Fetch→入库):30 行入缓存,qid/热度折算全对;一次性 smoke 测试已删,CI 恒离线。
- 热度折算:detail_text `^([0-9]+)\s?万热度$` → 万数×10000 存 points;渲染 points/10000 还原("zhihu.heat" 键)。非整万/非热度文案 → nil(行照列,元信息段缺省),无假 0。
- 行 URL 存规范页 `https://www.zhihu.com/question/<qid>`(Ctrl+O/Y 目标),不用 target.url 的 api 内部拼写。
- 热榜 feed 声明 ListSortPoints:榜返回序即热度序,UI 按快照重排,nil 热度沉底。
