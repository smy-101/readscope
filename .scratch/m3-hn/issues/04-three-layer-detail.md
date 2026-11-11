# 04: 三层详情 — 楼层 + 回复树

**What to build:** HN 帖子详情从单层扩成三层状态机:**正文层**(链接帖 = 一行原文链接;文本帖 = 渲染管线全文)→ **楼层层**(顶层评论各成一楼,楼层头 `author · score · N replies`)→ **回复层**(Enter 进入该楼的回复树)。深层回复压成两层、跨层带 `@parent` 前缀(纯函数);无回复的楼层不响应 Enter。←→ 在楼层间移动,Esc 逐层退。楼层/回复层的 Ctrl+N 为内存分页(开帖已拉全树,零额外请求)。首次开帖无缓存显示加载态;缓存先行渲染、在线后台静默刷新。Ctrl+O / Ctrl+Y 按层取正确目标(楼层/回复层 = 该评论的 HN 页 / 直链)。F1 帮助补键位。

**Blocked by:** 03

**Status:** ready-for-agent

- [x] 三层进出:Enter 进下一层(有内容时)、Esc 逐层退;层间焦点与滚动状态不丢
- [x] 楼层头 `author · score · N replies`;无回复的楼层按 Enter 不进入
- [x] 扁平化纯函数:任意深树 → 两层 + @parent 前缀;单测覆盖深嵌套 / 单枝长链 / 空树
- [x] 评论 HTML 走 M1 管线排版(段落/引用/链接编号),图片降级为标记
- [x] Ctrl+N 楼层/回复层分页纯内存;首次开帖显示加载态;再开帖缓存先行,后台刷新后楼层更新
- [x] 离线打开已缓存帖子:三层完整可翻
- [x] Ctrl+O / Ctrl+Y 按层取目标正确;F1 帮助与 zh-CN / en i18n 同步,键树一致

## Comments

**2025-12 由 03 号票评审转入**(实现时定夺):

- 空树语义:缓存里 0 行评论与"从未打开"不可区分——本票"首次打开无缓存显示加载态"会把开过但无评论的帖子每次都当首开重拉。需要 opened 标记或等价信号(数据面方案属本票)
  → **已定夺**:migration 006 新表 `detail_fetch`(item_id PK → items, fetched_at),在 `SaveComments` 事务内 upsert——标记与整树替换原子一致;`DetailFetched()` 读出。opened 且 0 评论 = 渲染缓存("无评论"),静默后台刷新
- 楼层头 score 无数据面:Algolia /items 端点评论节点 points 恒 null(03 实测定形),"author · score · N replies" 的 score 段需降级策略(不显示,或以 N replies 为主)
  → **已定夺**:分段拼接 + 空段丢弃(与热度雷达同规则),HN 现状渲染 `author · N replies`,score 字段留给有该数据的源;删除楼 author 走 i18n `[已删除]` 占位
- 树序已由 migration 005 seq 列结构性保证(父先于子),扁平化可直接前向扫描
  → **照此实现**:`buildFloors` / `flattenReplies` 均为单遍前向扫描;父 id 无匹配(现实不可能、缓存损坏可能)防御性提升为楼层,不丢评论

**2025-12 实现补充决策**:

- 楼层层为单楼视图(一次一楼:楼层头 + 正文),"该楼"无歧义,Enter/无回复不进入才可判定;←→ ±1 楼,Ctrl+N +10 楼快进(内存分页的楼层形态)
- 回复层为两层扁平化滚动文档:一层直答平铺,深层裹渲染管线自带缩进的 Quote(│ 条)+ `@parent` 前缀;Ctrl+N = 跳到下一回复首行;Ctrl+O/Y 目标 = 顶端可见回复(replyStarts 行归属,确定性)
- 每条评论的链接表保持局部:附录紧跟该评论下方(HN 评论 0–3 链接,就近可读),免掉跨评论链接重编号
- `DetailSource` 扩展 `CommentURL(id)`(hn 实现 `news.ycombinator.com/item?id=`,裸评论 id 与 item id 通吃):楼层/回复层 Ctrl+O/Y 与文本帖正文层(无外部 URL 时回落讨论页)取目标;ui 保持源无关
- 后台刷新语义:成功静默;失败时有缓存静默、无缓存(首开)才报 errors.detail_fetch,正文层始终可读
- 回复头在 @parent/author 之后附带相对年龄(`· 2h`,与正文 meta 同源):票面只规定了楼层头,回复头未规定,补上以支撑扫读;F1 补 `← →` 楼层导航行(spec Further Notes “新交互进 F1 帮助”的完形)

**2025-12 04 号票评审**(两轴并行,均 OK with notes):

- 修复:buildFloors 悬垂指针——id→指针存进增长切片,孤儿提升触发重分配后,更早楼层的后续回复写进死副本丢失(DFS 正常树不可达,恰为防御所针对的损坏缓存类);改 id→索引、用点重新解引用,回归测试钉住
- 修复:首开拉取失败腿补测(无缓存 + 死网:load_failed 脚注 + errors.detail_fetch 状态 + 正文可读 + 楼层占位);authorOrDeleted 收敛 4 处删除占位回退;openHNDetailCmd 变体去重测试脚手架;zh-CN errors 段冒号对齐全角
- 拒绝:楼层/回复字段组团再包 per-layer 结构(本就是一层的全部状态,包了只加间接);d.layer 多处 switch(惯用枚举分派,仅记录)
