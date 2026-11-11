# 02: 热度快照 + 按源元信息列

**What to build:** HN 列表行的元信息列显示 **"N pts · N comments · author"** 作为讨论热度雷达(值取 fetched 时刻快照,刷新时更新);RSS 行元信息列保持现状——元信息列的内容从此按源格式化。items 表经 migration 增加 points 与评论数两个快照列(RSS 行留空)。

**Blocked by:** 01

**Status:** ready-for-human

- [x] migration 可重放,旧库升级后 RSS 行为完全不变
- [x] HN 刷新写入 points / 评论数;再次刷新更新快照值
- [x] front/ask/show 列表按 points 序(= Algolia 返回序)渲染,new 保持时间序(01 转入:返回序渲染依赖本票快照列)
- [x] HN 列表行元信息列 "N pts · N comments · author";字段缺失优雅降级,不出现空段
- [x] RSS 列表与 F3 收藏列表行不受影响(回归)
- [x] 窄终端下元信息列截断规则与现有列表一致,不碎行

## Comments

**2025-12 实施**(待人工验收):

- migration `004_hn_heat_columns.sql`:`items` 增 `points`/`num_comments` 可空列,NULL = 无热度语义(RSS 恒空);从 v3 库升级既有行不动,重放幂等(`TestMigrateUpgradesOldDBPreservingRows`)
- `core.Item` 增 `Points`/`NumComments *int`(指针保住"缺失 ≠ 0");`core.Feed` 增 `ListSort`(零值=时间序,`points`=热度序),由源在 `Feeds()` 里声明——排序语义留在源缝,不落到 store
- hn 源:hit 解析 `points`/`num_comments`(缺字段 → nil);front/ask/show 声明 points 序,new 保持时间序
- store:`SaveFeedItems` INSERT/UPDATE 同步写两列(刷新即更新快照,`TestSaveFeedItemsSnapshotsHeat`);`FeedItems`/`StarredItems` 带出两列(收藏列表的热度雷达因此可用,快照 JSON 自动携带)
- ui:元信息列按行源格式化(hn → `hnMeta`,其余 → 相对时间;F3 混源逐行各显);列宽按全部行的最宽内容测量、夹在 [10, 32](10 = 旧固定时间列宽,RSS 渲染逐字节不变;32 上限即 spec 的"作者可缩略",超宽用 … 截断,过窄整列先于标题丢弃——沿用既有规则);`ListSortPoints` 的 feed 在 itemsMsg 落地时按快照稳定排序(无热度行沉底)
- i18n:`hn.pts`/`hn.comments` en+zh-CN 同步;GLOSSARY 增**热度快照**
- 回归:既有 RSS 列表/收藏/窄终端测试全部通过;refresh tracer 扩展验证了真实刷新周期里热度快照的写入与更新
