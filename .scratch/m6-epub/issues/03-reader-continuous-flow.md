# 03: 全屏阅读器 — 连续流/TOC/续读位/书签/宽度

**What to build:** `internal/ui/reader.go` 独立全屏(ADR #11):左 TOC 侧栏 + 右 viewport。章节 XHTML → 既有渲染管线 → 全书**连续流**分页(Q3:翻页无缝跨章,章界 = 章标题+分隔线,TOC 无项的章也在流里);按章惰性渲染 + 窗口缓存,大书不爆内存;**续读位** `(spine_index, block_offset)` 读写(落盘节流),换宽重排锚点不丢,重启精确落回;Ctrl+B 书签+可选笔记(复用输入对话框)+列表跳转;±宽度调节(text_width=0 随终端);图片 `[图片: alt]` 降级 + 外部打开(解 zip 落临时文件+系统程序);Ctrl+N 跳下一章头;Esc 回书架;F1 帮助更新。

**Blocked by:** 02(书架屏供入口与书目)。

**Status:** pending

- [ ] 左 TOC(层级缩进,当前章高亮)+ 右 viewport;TOC 跳转落章头;01 的标题生成兜底章名可用
- [ ] 连续流:跨章翻页无停顿;章界呈现 = 章标题+分隔线
- [ ] 惰性渲染+窗口缓存:跳章不全书渲染,往返阅读缓存命中不重复渲染;内存策略票内定并有测试
- [ ] 续读位:退出/重启 → Enter 精确落回同块;±宽度/终端 resize 重排后 block_offset 不变(锚点不丢)
- [ ] pct 显示与书架屏一致(同一 progress 行)
- [ ] 书签:Ctrl+B 加(可选笔记)、列表、跳转、删除;键位进 F1 帮助
- [ ] 宽度 ± 调节即时重排;text_width=0 随终端 resize
- [ ] 图片标记 Enter/Ctrl+O:解 zip 落临时文件+系统程序打开;svg 章节按未知标签处理,fixture 钉行为(不 panic、有降级)
- [ ] 书文件被移走/损坏:明确状态栏错误,不崩;Esc 可全身而退
- [ ] `assertLinesFit` 覆盖 reader 行组装;i18n 双语组合成品断言
