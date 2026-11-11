# 02: 源包骨架 + 热榜 feed — 注册→拉取→入库→列表

**What to build:** `internal/source/zhihu` 包落地,id `"zhihu"`,按"加源 = 加一个包"注册进源注册表;边栏固定 热榜 feed(Following/日报 在 03/05 票进);F5 从 `api.zhihu.com/topstory/hot-list` 拉全量(≤30 条,免登录,仅桌面 UA),问题卡作为离线快照写入 items(`zh:question/<qid>`,qid 从 target.url 提取),列表立即显示;元信息列显示热度(detail_text 折算进 points 热度快照列,呈现还原 "2045 万热度");无翻页(非 PagedSource,Ctrl+N 提示"没有更多了")。● 未读点/Ctrl+R/F4/Ctrl+A/F3 沿用源无关机制。DefaultSources 改 `["rss","hn","zhihu"]`,已存在的 sources.toml 不自动改写(README 记手动加法)。

**Blocked by:** 01 (数据面判定至少热榜 GREEN)

**Status:** ready-for-agent

- [ ] Tab 切到知乎:边栏出现热榜 feed,标签双语,i18n 键树一致
- [ ] F5 拉热榜:问题卡入库为快照;id 确定性,重复刷新不产生重复行、不触碰已读状态(跨 feed 重归属机制复用)
- [ ] qid 从 target.url 提取;fixture 含超 2^53 裸整数 id,不丢精度
- [ ] 元信息列 "N 万热度"(points 快照折算/还原);非 question 卡与缺 title 的行跳过不崩,错误信息带 feed 名
- [ ] ● / Ctrl+R / F4 / Ctrl+A 对知乎条目与 RSS/HN 行为一致
- [ ] httptest 假热榜端点(fixture JSON)集成测试,离线可跑(CI 无网络依赖)
- [ ] DefaultSources 含 zhihu;无 cookie 场景热榜照常(免登录)
