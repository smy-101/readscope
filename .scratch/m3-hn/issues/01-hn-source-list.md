# 01: HN 源包 — 四类 feed + 刷新进缓存

**What to build:** 用户 Tab 切到 Hacker News,边栏出现 Front / New / Ask HN / Show HN 四类 feed;F5 从 Algolia 公共索引拉当前 feed 的首页,帖子作为离线快照写入 items,列表立即显示——M3 的第一条 tracer bullet:注册 → 拉取 → 入库 → 列表可见。hn 源包按"加源 = 加一个包"落地(与 rss 同款模式);条目 id 形如 `hn:algolia:<storyID>`,链接帖 body 存一行文章链接,文本帖存 story_text(HTML,后续走渲染管线)。HN 刷新恒为无条件(Algolia 无 ETag 语义)。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] Tab 切到 HN:边栏四类 feed,标签双语(zh-CN / en),i18n 键树一致
- [x] 四类 feed 分别映射 `front_page` / `search_by_date` / `ask_hn` / `show_hn`,列表按返回序渲染 →映射与入库已落地;front/ask/show 的返回序=points 序排序已随 02 交付(feedDefs 声明 ListSortPoints,列表按快照列稳定排序;02 号票测试覆盖)
- [x] F5 拉当前 feed:链接帖/文本帖入库为快照;id 确定性,重复刷新不产生重复行、不触碰已读状态(跨 feed 同帖重归属已测)
- [x] ● 未读点、Ctrl+R、F4、Ctrl+A 对 HN 条目与 RSS 行为一致(沿用源无关机制,不另造)
- [x] httptest 假 Algolia(fixture JSON)集成测试,离线可跑(CI 无网络依赖)
- [x] 单 feed 失败不中断其余源;hit 缺 url/author 等字段不崩,错误信息带 feed 名
