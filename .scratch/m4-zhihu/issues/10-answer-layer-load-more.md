# 10 — 回答层尾部加载更多(hop-then-fetch 同构补齐)

Status: done (2026-12-17,agent 侧;红→绿 TestZhihuAnswerLayerLoadsMoreAtTail,全量 go test ./... 绿;剩余人工手感走查归用户真网环节)

## What

知乎问题三层详情里,回答层(zhihu answer layer)的 Ctrl+N 目前是纯内存跳楼层
(floorSkip),←→ 到已加载末尾即顶死——加载下一页回答必须 Esc 退回问题层再
Ctrl+N。用户报:浏览回答过程中无法就地加载更多,繁琐(2026-12-17 报障:
「只能查看5个评论」,即回答层首页 limit=5 的 5)。

## Why(诊断证据,2026-12-17)

- 真网 + 真 cookie 复验:回答层 p1=5/p2=5、评论层 p1=26/p2=31,两层源层翻页全通;
  probe 11/11 GREEN;refresh 合并、UI floorsMsg 追加、pager 探测链路代码与测试均正常。
  「卡在 5」不是接线 bug,是层位语义缺口:加载更多的入口只存在于问题层。
- 评论层早已有同构规则(知乎/HN 回复层):窗口内还有已加载行就跳行,全部可见
  或到底就拉下一页——一个键,语义随位置切换。回答层补同一条规则即可。

## Decision

- 回答层 Ctrl+N 改为 hop-then-fetch:当前楼层之后还有已加载楼层 → 跳 floorSkip
  (原行为不变);已在已加载尾部且 feeds 游标未尽(游标非空且未达 answer_count)
  → 就地 moreFloorsCmd 拉下一页,落帖不自动跳层(与回复层落帖语义一致),状态栏
  报「已加载 N 个回答」。
- → 在尾部且游标未尽时,状态栏提示 Ctrl+N 可加载更多(可发现性)。
- HN(floor 无 pager)与文章(单伪楼层、无游标)行为不变。
- 顺带记录(非本票范围):真网实测 root_comment 每页已从 06 号票的 20 缩到 10
  (第三方 API 文档同口径);readscope 走游标翻页,不受影响。

## Comments

- **2026-12-17 立票**。诊断回路:`.scratch/m4-zhihu/diag/main.go`(一次性,验收后删)
  驱动 Detail/FetchFloorsPage/FetchAnswerComments 真 p1+p2;基线 probe 样本在
  `samples/`。真网偏差回写:无(门矩阵 MATCH,仅页大小漂移)。
- **2026-12-17 落地**。TDD 红→绿:`internal/ui/zhihu_detail_test.go` 增
  `TestZhihuAnswerLayerLoadsMoreAtTail`(中段仍跳层 / 尾部→提示 / 尾部 Ctrl+N
  就地拉页且不跳楼 / 游标尽→no more),实现在 `internal/ui/detail.go`
  (layerFloors 分支 + `floorsPageRemain` 门 + →尾部提示),i18n 增
  `zhihu.answers_tail` 双语。GLOSSARY 回答层词条同步;诊断 harness 已删;
  `go test ./...`、`go vet`、`gofmt` 全绿。README 原文已写「Ctrl+N 加载下一页回答」,与行为对齐,无需改。
