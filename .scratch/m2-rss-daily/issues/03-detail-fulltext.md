# 03: 详情层 — 全文阅读

**What to build:** 用户在列表选中一条按 Enter,列表区被详情层覆盖:正文用 M1 渲染管线排版(标题/段落/引用/代码高亮/中英混排折行/禁则),链接收集编号(正文 [n]、文末列 URL),上下键滚动阅读,Esc 退回列表。Ctrl+O 用系统浏览器打开原文,Ctrl+Y 复制链接。正文读的是 items.body 快照 —— 断网可读(M2 验收标准后半句)。

**Blocked by:** 02(TUI 列表屏 — 边栏真实 feeds + 缓存先行列表)

**Status:** ready-for-human

- [x] Enter 进详情层 / Esc 退回,焦点与选中位置保持
- [x] items.body(HTML 快照)走 M1 管线:Block 流 → 折行 → 样式;无 HTML 的纯文本正文也正确显示
- [x] 链接编号:正文内 [n],文末列出 URL 清单
- [x] 图片降级为 `[图片: alt]` 标记(管线既有行为,详情层验证)
- [x] 上下滚动流畅;窗口 resize 重排不错位
- [x] Ctrl+O 系统浏览器打开原文;Ctrl+Y 复制链接并有状态栏反馈
- [x] 一条真实中文 RSS 全文(阮一峰周刊)目检排版正确

## Comments

- 管线补齐:M1 只在文末附录编号,本票在 `render` 管线解析期给每个带 href 的 `<a>` 追加
  可见 ` [n]` 标记(dom.go),goldens 已重生成;`#anchor` 链接不编号。
- 详情层 `internal/ui/detail.go`:标题(Heading1)+ 作者/相对时间 + 分隔线的头部块前置于正文
  Document;正文含 `<` 走 ParseHTML,纯文本按行成段(空行合并为块间距),空正文显示占位文案。
  滚动窗口钳制在 [0, 末页];resize 时按新宽度重排并钳制位置。
- 键位:阅读中 ↑↓/PgUp/PgDn/Home/End 滚动,Ctrl+O/Ctrl+Y 动作,F1/Ctrl+T/F5/Ctrl+Q 照常,
  Tab 切源会关闭详情层;帮助覆盖层优先于阅读键。
- `internal/browser`:WSL 优先 `wslview`(xdg-open 够不着 Windows),`READSCOPE_BROWSER` 覆盖一切。
- Ctrl+Y 用 OSC 52(go-osc52,SSH 下也可用,无 xclip 依赖);两者都有状态栏成功/失败反馈。
- 目检:用周刊风格全文(标题/科技动态/工具/代码块/图片/言论/列表/7 链接)在 100×34 终端
  dump 实际 TUI 视图逐屏核对:中英混排折行、禁则(行首无句读)、引用竖线、代码原样、
  `[图片:冰岛灯塔]`、附录 [1]–[7] 全部正确;真机终端上的手感验收归 07。
