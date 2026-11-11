# 03: 知乎日报 feed — 列表 + 全文详情 + 离线

**What to build:** 边栏加 知乎日报 feed(免登录)。F5 从 `news-at.zhihu.com/api/4/news/latest` 拉当日条目(`zh:daily/<storyId>`),元信息列 = hint("作者 · 2 分钟阅读");Enter 走 DetailSource:拉 `api/4/story/{id}` 的 body(HTML)存入 body 快照,经 zhihu 包 DOM 清洗后进 M1 渲染管线;二次打开渲染缓存零请求,离线可读。与 RSS 全文同形(正文随缓存走)。日报无评论层,Enter 已在全文层。

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] 边栏 知乎日报 feed,标签双语;F5 拉当日条目入库,id 确定性
- [ ] Enter 打开:story body 拉取→DOM 清洗→渲染管线排版(标题/正文/图片降级标记)
- [ ] 正文进 body 快照:离线重启后已开条目可读,未开条目离线时明确提示
- [ ] 元信息列 = hint;缺 hint/图片的条目不崩
- [ ] httptest 假日报端点(列表 + story fixture)集成测试,离线可跑
- [ ] 清洗 golden 样例过 M1 管线快照测试(日报 HTML 子集)
