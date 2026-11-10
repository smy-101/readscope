# 01: `readscope rss refresh` — 订阅拉取入缓存

**What to build:** 用户在 `sources.toml` 里写好 RSS/Atom 订阅(label + url),运行 `readscope rss refresh`,程序逐源拉取、解析,把条目作为离线快照写入 SQLite(items + FTS 同步),并在终端打印每个源的新增条数。这是 M2 的第一条 tracer bullet:配置 → 拉取 → 入库,完全独立于 TUI 可验证。领域模型(Item/Feed)与 Source 接口/注册表随本票落地,形状遵循 PLAN §4 的接口定义。

**Blocked by:** None (can start immediately)

**Status:** ready-for-human

- [x] `sources.toml` 可读可写:enabled 顺序 + rss.feeds(label/url);缺失时给空默认,格式错明确报错
- [x] Source 接口 + 注册表落地,RSS 是第一个实现;加源 = 加一个包
- [x] 条目 ID 按源确定性生成(`rss:<feedkey>:<guid>`),重复刷新不产生重复行
- [x] items 入库为快照(body 全文),FTS5 同步可搜;已有条目不覆盖已读状态
- [x] httptest 假服务器喂 RSS/Atom 样例的集成测试,离线可跑(CI 无网络依赖)
- [x] `readscope rss refresh` 对每源打印 新增/总数;某源失败不中断其余源,失败信息清晰

## Comments

**2025-11-10 · agent:** 已实现,待人审阅(状态改 ready-for-human)。

落点(均按 PLAN §4/§5 形状):

- `internal/core/model.go` — Feed/Item 领域模型(列与 items 表一一对应)
- `internal/core/sources.go` — sources.toml 读写:缺省 → `enabled=["rss"]`+空 feeds;文件省略 enabled 时也回退 ["rss"],显式 `enabled = []` 则尊重;坏格式报错带路径。Save 供 06 OPML 导入用
- `internal/source` — Source 接口(ID/Feeds/Fetch)+ 注册表;实现包 init 自注册,cmd 侧 import 即生效(加源=加包成立,`source_test.go` 用假源验证注册表本身)
- `internal/source/rss` — 手写 HTTP 抓取(UA/超时/10MiB 上限,为 05 号 ETag 留了单一扩展点)+ gofeed 解析;id = `rss:<feedkey>:<guid>`,guid 缺失回退 link,再回退 title+published 的 sha1;body 取 content:encoded 优先、description 兜底;Atom 的 `<updated>` 填 published_at
- `internal/store/items.go` — `SaveFeedItems` 单事务:新条目 INSERT、旧条目按 id UPDATE、FTS 行删后重建;read_state 独立表、本函数永不触碰(测试证明刷新后 bit 不变);同批重复 guid 保留首条
- `internal/store/fts.go` — FTS 存的是去标签纯文本(script/style 丢弃、实体解码、空白折叠),markup 不会污染搜索;解析失败回退原文,索引永不因脏 feed 失败
- `internal/refresh` — 编排 + 分离的 Report(本地化输出);单 feed 失败只记入该行,不中断其余
- `cmd/readscope/rss.go` — 子命令接线;任何 feed 失败 exit 1;新增 [rss] i18n 键(en/zh-CN,奇偶测试保一致)

验证:`go vet`、`go test ./...`(含 -race)、真二进制冒烟(本地 http.server 喂 RSS:首刷 2 new、二刷 0 new、404 源隔离失败、zh 输出、无订阅提示、坏 TOML 报错、断网后 SQLite/FTS 可读)。

给后续票的两个发现:

1. **FTS5 unicode61 不切 CJK**:连续汉字串成一个 token,搜「离线」匹配不到「离线快照正文」(前缀 `离线*` 可命中)。数据已入库,属分词粒度问题;M4 搜索里程碑需定分词方案(trigram 或自定义 tokenizer)。英文搜索正常。
2. **modernc 驱动时间编码**:time.Time 写入后,经 `MAX()` 聚合读出是 string(直接列读取待验证)。02 号 TUI 读 published_at 时留意统一读法。
