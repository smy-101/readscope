# 02: 热度快照 + 按源元信息列

**What to build:** HN 列表行的元信息列显示 **"N pts · N comments · author"** 作为讨论热度雷达(值取 fetched 时刻快照,刷新时更新);RSS 行元信息列保持现状——元信息列的内容从此按源格式化。items 表经 migration 增加 points 与评论数两个快照列(RSS 行留空)。

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] migration 可重放,旧库升级后 RSS 行为完全不变
- [ ] HN 刷新写入 points / 评论数;再次刷新更新快照值
- [ ] front/ask/show 列表按 points 序(= Algolia 返回序)渲染,new 保持时间序(01 转入:返回序渲染依赖本票快照列)
- [ ] HN 列表行元信息列 "N pts · N comments · author";字段缺失优雅降级,不出现空段
- [ ] RSS 列表与 F3 收藏列表行不受影响(回归)
- [ ] 窄终端下元信息列截断规则与现有列表一致,不碎行
