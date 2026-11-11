# 05: ETag 条件请求

**What to build:** `readscope rss refresh` 对每个 feed 记住上次响应的 ETag / Last-Modified,下次请求带上条件头;服务器答 304 时不解析不写入,刷新近零流量。这是拉取层的纯增强,与 TUI 票并行。

**Blocked by:** 01(`readscope rss refresh` — 订阅拉取入缓存)

**Status:** ready-for-human

- [x] feed 级条件状态存储(etag/last-modified/last_fetched),走 schema migration
- [x] 请求带 If-None-Match / If-Modified-Since;304 → 跳过解析,计为"未变"
- [x] 200 → 更新快照与条件状态
- [x] httptest:同一服务器两次 refresh,第二次 304,items 无新写入;ETag 变化后 200 正常更新
- [x] `rss refresh` 输出区分 新增/未变/失败
- [x] 服务器不支持条件请求时行为退化为普通刷新,无副作用
