# 03: 全屏阅读器 — 连续流/TOC/续读位/书签/宽度

**What to build:** `internal/ui/reader.go` 独立全屏(ADR #11):左 TOC 侧栏 + 右 viewport。章节 XHTML → 既有渲染管线 → 全书**连续流**分页(Q3:翻页无缝跨章,章界 = 章标题+分隔线,TOC 无项的章也在流里);按章惰性渲染 + 窗口缓存,大书不爆内存;**续读位** `(spine_index, block_offset)` 读写(落盘节流),换宽重排锚点不丢,重启精确落回;Ctrl+B 书签+可选笔记(复用输入对话框)+列表跳转;±宽度调节(text_width=0 随终端);图片 `[图片: alt]` 降级 + 外部打开(解 zip 落临时文件+系统程序);Ctrl+N 跳下一章头;Esc 回书架;F1 帮助更新。

**Blocked by:** 02(书架屏供入口与书目)。

**Status:** done

- [x] 左 TOC(层级缩进,当前章高亮)+ 右 viewport;TOC 跳转落章头;01 的标题生成兜底章名可用
- [x] 连续流:跨章翻页无停顿;章界呈现 = 章标题+分隔线
- [x] 惰性渲染+窗口缓存:跳章不全书渲染,往返阅读缓存命中不重复渲染;内存策略票内定并有测试
- [x] 续读位:退出/重启 → Enter 精确落回同块;±宽度/终端 resize 重排后 block_offset 不变(锚点不丢)
- [x] pct 显示与书架屏一致(同一 progress 行)
- [x] 书签:Ctrl+B 加(可选笔记)、列表、跳转、删除;键位进 F1 帮助
- [x] 宽度 ± 调节即时重排;text_width=0 随终端 resize
- [x] 图片标记 Enter/Ctrl+O:解 zip 落临时文件+系统程序打开;svg 章节按未知标签处理,fixture 钉行为(不 panic、有降级)
- [x] 书文件被移走/损坏:明确状态栏错误,不崩;Esc 可全身而退
- [x] `assertLinesFit` 覆盖 reader 行组装;i18n 双语组合成品断言

## Comments

**票内定拍板(2026-11-20,并工前四问)**:
1. **内存窗口 = LRU 16 章**:渲染章 LRU 逐出,视口跨章期间在用章永不逐(超帽视口照单全收,内存随视口不随书);被逐章重进重解析。
2. **落盘节流 = 章界+退出**:锚点 spineIndex 变化即写;Esc/Ctrl+Q 退出必写。崩溃最多丢当前章内位置,零定时器。
3. **章界呈现 = 分隔线+标题**:每章 [Rule, Heading(章名)] 前缀块,章名兜底链 TOC 匹配 → 章内首个标题 → 文件名;TOC 名与章内标题相同则去重(比较经 `render.SqueezeCJK` 对齐管线的 CJK 空格挤压)。
4. **键位包整包照提**:←→/Tab 焦点、Enter(TOC)跳章、Enter(正文)/Ctrl+O 外开最上可见图片、Ctrl+N 下一章头、Ctrl+B 书签(空笔记可)、Ctrl+G 书签列表(Enter 跳/Ctrl+D 删)、+/- 宽度±2 下限20、Home/End 书首尾、Ctrl+S/F 吞掉留票04、Esc 退书架;F1 载阅读模式专属键位表。

**实现落点(2026-11-20 收口)**:切片 1–11 逐红绿提交(6e49b7e 起):store progress/bookmarks DAO(书签以位置为身份,重加刷笔记保 created_at)→ epub.ReadItemIn(章内相对解析,嵌套章图片的必经缝)→ 流引擎 bookFlow(feeder 缝,缝合视口带属主标注、锚点=顶行所在块、块间分隔行归前块、书尾钳制、pct 内容加权不含装饰块)→ ReaderFlow fixture(5 章已知几何/svg 章/坏章/嵌套目录图片)→ epubFeeder(标题链+降级占位)→ UI 开合/全屏视图/键位泵/宽度/书签/图片外开/F1。

**实现期钉住的语义**:
- **锚点坐标系**:block_offset 索引章的流块序(0=分隔线,1=章题,2+=内容块);换宽/续读都锚到块首行。
- **pct = (章序+章内内容块比)/章数**,内容比排除两个装饰块;视口达书尾行 = 1.0(读完显示 100%)。
- **盘中途损坏**:feeder 降级占位进流保持可读,degraded/lastErr 记账供键泵上状态栏(明确状态栏错误)。测试用 truncate 模拟——已开 fd 的 unlink 在 Unix 不影响读,原移走测试的通过是 fixture 坏章的巧合(评审逮住)。
- **图片外开**:视口最上 `[图片]` 标记 → 属主章 doc.Links → http(s) 直开浏览器,否则 ReadItemIn 解 zip → 内容哈希临时文件(复用不删)→ 系统程序。
- **Ctrl+Q 在阅读器内 = 落盘后照常全局退出**(节流的"退出"含退出应用,非 creep);书架 Esc 后行重载零重扫(裁决/failErr 保留)。

**偏差(批准包之外,已记)**:
1. ctrl+= 无控制字节不可绑定 → 减宽别名 = `ctrl+_`(终端字节层 ctrl+-,0x1F);加宽仅裸 `+`。
2. Tab 为翻转语义(首按进 TOC);shift+tab 别名经评审撤除(未在批准包)。
3. 同锚点重加书签 = 原地刷新笔记(PK 位置身份的必然选择,切片 1 拍板)。

**双轴评审(2026-11-20,workflow 并行子代理,diff = 7cd6528...HEAD)**:Standards 轴 2 硬项已修(i18n zh 组合断言补齐 bm_saved 含动词串;`bookPaneWidth`/`lastTop`/`scrollBook` 三组三处同形提炼)+ Spec 轴补齐(盘中途损坏上状态栏 + feeder 虚注释修正;pct 内容加权;模态行按预算截断 + 三尺寸整屏宽度守卫)。评审另确认行宽不变量/注释声明可验合规。**挂账容忍对**(两处未到提炼线,再添一处即提炼):epubFeeder.Chapter 内联标题链 vs title() 记忆化链、SqueezeCJK vs squeezeCJKSpaces(同规则两形);**deferred**:锚点 (ch, block) Data Clump 可提炼 `Anchor` 小类型(评审判断题,未做)。修复后全量 go test ./... 绿,gofmt/vet 净。

**留给票 04 的缝**:Ctrl+S/F 已吞并占提示;文本投影走 flow 缓存章的 doc 与 Lines.Plain()(文本投影一致性「搜到的=看到的」);跳转目标 = jump(ch, block)。
