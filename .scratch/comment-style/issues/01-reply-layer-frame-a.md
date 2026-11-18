# 评论层样式:帧 A(缩进阶梯)折回

Status: done (2026-11-14 agent 侧, 全量 `go test ./...` 绿, i18n parity 验讫; 2026-11-18 用户真机手感走查**暂时通过**, 改良方向延后——见 Comments)

## 问题

知乎(及共用回复层渲染的 HN/heybox/专栏)评论层一屏里,三个东西分不清:

1. **作者 vs 正文** —— 头行(作者·时间)与正文行都是一坨文字,作者仅靠 bold;
2. **谁回复谁** —— `@王五 赵六` 靠 dim/bold 微差区分回复对象与发言者,扫读读不出;
3. 更深层压平成两级后,回复链只剩这个前缀可依赖。

## 决策

原型四帧(现状 / A 缩进阶梯 / B 颜色分层 / C meta 弱化)对比后,**用户选定帧 A**:

- 深层(depth≥2)整块缩进两格,引用条(`│`,Quote 块)退场——缩进即层级视觉;
- 头行改为 `↳ 作者 回复 父作者 · 时间`(`detail.reply_to`,en "replied to %s"),回复对象从"@前缀猜"变为明示;
- 作者名上主题色(`SpanMarker|SpanBold`,accent),楼层头(`floorHeaderSpans`)同步——名字与正文跨层可分;
- 删除行维持 dim `[已删除]` 占位,回复已删除父时渲染 `↳ X 回复 [已删除]`。

**一手证据**:原型全部四帧留档于分支 `prototype/comment-styles`
(`internal/ui/protocomment`,检出后 `go run ./internal/ui/protocomment`),
含 fixture(三层链 / 已删除行 / 已删除父)与四帧实现;主干不保留该目录。

## 折回范围

- `internal/ui/detail.go`:`replyDoc` 重写(非照搬)、`layoutReplies` 缩进、
  新增 `indentLines`、`floorHeaderSpans` 作者上色;
- `configs/i18n/{en,zh-CN}.toml`:`detail.reply_to`;
- 测试同步:hn / zhihu detail / zhihu article / heybox 四处断言
  (`@bob carol` → `↳ carol replied to bob`、quote bar → indent 等);
- GLOSSARY(扁平化、评论层词条)与两份 README 的呈现描述更新;
  `docs/PLAN.md` 为历史里程碑记录,不改。

## 落选帧的去向

B（结构不动纯上色）与 C（meta 整行弱化）未销毁，仍在 prototype 分支可随时重开。

## Comments

**评审（2026-11-14，Standards+Spec 双轴，并行子代理，diff = 工作区 vs HEAD）**：两轴均出发现货问题，已随批修复。

- **P1（两轴独立收敛）**：深层行先按 `w=listWidth-1` 折行再加 2 格缩进，实际占宽 `listWidth+1`，比消费端 lipgloss 定宽宽 1 列——恰折满的深层行会被再折一字破坏列对齐。已修：`row.deep()` 行按 `max(1, w-2)` 布局后再加前缀；`indentLines` 注释与实现自此一致。
- **P1（Standards）**：ADR #13 结论列的 @parent 被静默替换，违反 domain.md「ADR 冲突须显式标出」；票面「PLAN.md 不改」的声明让位于仓规范。已修：PLAN.md #13 行与 §8 M3 行各加 2026-11-14 修订注（沿「决策 #14 修订」先例）。
- **P2 四项全修**：抽 `authorSpan`（author 样式三行两处重复）；`replyRow.deep()` 单点判定 depth≥2（头行与缩进两处不再各自摆开关）；i18n 键 `detail.reply_to`→`detail.replied_to`（与 `replies_to` 一字母之差易错抓）。
- **放行项（Spec b）**：floors.go / heybox / zhihu 三处纯注释的术语退役超出折回范围清单，零行为变化，随批知会。

修复后全量 `go test ./...` 16 包绿，gofmt/vet 净。

**用户判定（2026-11-18）**：帧 A 真机手感**暂时通过**；后续再评估改良方向（用一段时间后再定，候选：缩进量/颜色对比/前缀形式；落选帧 B/C 仍在 `prototype/comment-styles` 分支可重开）。本票关闭，改良不另开票——到时按新问题走 `/grill-with-docs`。
