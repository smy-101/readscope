# M6: EPUB 书库+阅读器 — Spec

Status: ready-for-agent

上游输入:`research-epub-libs.md`(2026-11-18 双库评估,行为探针实证)、`probe/`(可复跑探针,嵌套模块)、`ftsprobe/`(FTS5 中文事实探针)、Grill 共识(2026-11-18,三轮八项定盘);术语见 `GLOSSARY.md` 书库节;PLAN §2 ADR #17。

## Problem Statement

信息流侧(M0–M5)已收官:RSS/HN/知乎/小黑盒可刷可读。PLAN 定位的另一半——本地 EPUB 书库+全屏阅读器——尚未动工:`internal/epub`、`internal/library` 还是 M0 空占位,`books/progress/bookmarks` 三表建好但零读写,UI 无书架屏与阅读器。前置风险「解析库选型」已评估完毕:两个候选都不满足(Spine 容错贴需求但 GPL-3.0 与 MIT 分发冲突,一票否决;billdaws 脏书硬失败、`./` 脏 href 读不到章节、DRM 静默乱码——探针实证),按 PLAN 预案走自写薄层(ADR #17)。FTS5 探针另证:默认 unicode61 中文子串搜索全灭,真要建索引得上 trigram+2 字词 LIKE 回退——叠加「书内搜索本无需索引」的判断,Q7 定扫描式,FTS5 表延后。

## Solution

新增书库域:书库目录递归扫描即上架、删文件即下架(ADR #10),`add` 子命令拷入;书架屏作源栏入口(标题/作者/进度%/最近阅读,按最近阅读排序);Enter 进独立全屏阅读器(ADR #11:左 TOC + 右 viewport)。整本书经既有渲染管线(M1 资产)排成**连续流**——全书一条 Block 流,翻页无缝跨章;**续读位** `(spine_index, block_offset)` 与字号/终端宽/换机无关,重启精确落回;书签带笔记;±宽度调节;**书内搜索**为扫描式子串匹配,零索引,结果给章节+摘录并 block 级跳转。解析层自写薄层(~400–700 行):partial + warnings 照常开书,encryption.xml 预检——真 DRM 报「不支持」,字体混淆放行。零 schema migration。

## User Stories

1. As a reader, I want 书架 as a source-rail entry, so that books sit beside feeds in one app.
2. As a reader, I want books already in my library dir to appear after a scan, so that there is no import ceremony.
3. As a reader, I want `readscope add book.epub` to copy the file into the library, so that the dir stays the single source of truth.
4. As a reader, I want deleting a file to unshelve the book, so that managing books stays in the file manager.
5. As a reader, I want title/author/progress%/last-read columns sorted by recent reading, so that 在读书 stays on top.
6. As a reader, I want Enter to drop me at my 续读位, so that reopening a book resumes exactly where I left.
7. As a reader, I want a fullscreen reader with TOC on the left, so that long books are navigable.
8. As a reader, I want seamless paging across chapter boundaries, so that reading never breaks at chapter ends.
9. As a reader, I want my position to survive width changes, restarts and machines, so that progress is never lost.
10. As a reader, I want Ctrl+B to bookmark with an optional note, so that I can come back to a passage.
11. As a reader, I want Ctrl+S to search the whole book, so that I can find any passage again.
12. As a reader, I want search results to show chapter + snippet and jump to the passage, so that hits land where I read.
13. As a reader, I want +/- to adjust reading width, so that line length suits my eyes.
14. As a reader, I want images as [图片: alt] markers openable externally, so that illustrated books stay readable.
15. As a reader, I want code blocks highlighted and 中文折行带禁则, so that tech books read well (既有管线资产).
16. As a reader, I want dirty books to open with warnings instead of failing, so that 旧书少个字段也能读.
17. As a reader, I want DRM/损坏 books to fail with a clear 「不支持/文件损坏」 error, so that I know it's the file, not the app.
18. As a reader, I want offline reading by default, so that commute reading never needs network (本地文件天然离线).
19. As a bilingual user, I want every new UI string in both packs, so that the app stays uniformly bilingual.
20. As a maintainer, I want golden fixtures (合成书+坏书) in git, so that parsing regressions go red in CI.
21. As a user, I want doctor to report library health, so that self-check covers the book domain.

## Implementation Decisions

- **解析层自写**(ADR #17,`internal/epub`):`Open(path) → Book`(title/authors/language、spine、manifest、TOC、warnings)+ `ReadItem(href)` 原始字节。行为规格抄语义不抄代码:Spine 的 partial+warnings 模型与坏书 golden 清单、readium 的 encryption 判定口径、goread 的真实世界 hack(OEBPS 盲试、`./` 剥离、latin1 CharsetReader)。零新依赖(archive/zip + encoding/xml + 既有 x/net/html)。
- **容错语义**:唯二硬失败 = 损坏 zip、真 DRM;其余(缺 container、OPF 非良构/缺 version/缺 ns、缺 NCX、缺 spine)全部降级为 warning 照常开书——缺 container → zip 扫 `*.opf`;OPF 垃圾 → manifest/spine 从 zip 推断、TOC 从章节标题生成(Spine 实证语义)。
- **DRM 预检**:开书先查 `META-INF/encryption.xml`;算法白名单(`idpf.org/2008/obfuscation`、`adobe.com/pdf/enc#RC`,字体混淆)放行,其余 → 类型化 `ErrDRMUnsupported`,UI 报「不支持」(readium 判定口径)。
- **href 归一**:`path.Clean` + 剥 `#fragment` + %XX 解码(探针实证 billdaws 栽在 `./`);spine idref→manifest join 自持。
- **TOC 双路**:EPUB3 nav 优先(`epub:type=toc` 过滤,防 landmarks 误选),NCX 兜底;嵌套保留层级;皆无 → 章节标题生成 + warning。
- **书库**(`internal/library`):递归扫 `*.epub`;books upsert(path 唯一,元数据走解析层);盘上消失的行清除 = 下架(progress/bookmarks 随 CASCADE);扫描时机 = 进书架屏 + F5。
- **书架屏**:源栏 [书架] 入口;列 = 标题/作者/进度%/最近阅读时间;排序 progress.updated_at desc,无 progress 按 added_at 垫后;Enter 开阅读器;屏内无删除操作。
- **阅读器**(`internal/ui/reader.go`):独立全屏,左 TOC 侧栏 + 右 viewport;章节 XHTML → 既有渲染管线 → Block 流 → 连续分页;按章惰性渲染 + 窗口缓存,大书不爆内存(缓存策略归票);Esc 回书架;Ctrl+N 跳下一章头。
- **续读位**:progress 表既有 `(spine_index, block_offset)` 主锚点,pct 仅显示用;落盘时机节流(归票);±宽度触发重排,锚点沿 Block 流不变。
- **书签**:bookmarks 表既有;Ctrl+B 当前位置 + 可选笔记(复用输入对话框);书签列表跳转,键位随 F1 帮助定稿。
- **搜索**(Q5/Q7):扫描式——已渲染章用缓存文本投影,未渲染章 `ReadItem` 现读;**文本投影与屏显一致**(同一管线剥文本,搜到的=看到的);结果 = 章节名+摘录(命中前后 N 字),Enter 跳章滚到命中块,Esc 回结果列表续选;ASCII 大小写不敏感,CJK 直配;状态栏「搜索中」。
- **图片**:管线既有 `[图片: alt]` 降级;外部打开 = 解 zip 落临时文件 + 系统程序(细节归票);svg 不在管线白名单,按未知标签处理,fixture 钉行为。
- **存储**:**零 migration**(三表已就位);无 FTS、无新表。
- **i18n**:新键 zh-CN/en 双语言包,完整性测试覆盖;doctor 加书库行(书数/最近扫描/解析警告数)。

## Testing Decisions

- 只测外部行为;CI 恒离线;真书验收归用户真机环节。
- **合成 EPUB fixture 进 git**:纯 Go 构造 zip(testdata 生成器),覆盖中文禁则/代码块/图片/嵌套列表/多级 TOC/latin1 OPF;golden 断言解析结果与渲染行模型。
- **坏书对照集**:按 Spine 公开 fixture 类型自造同类样本(malformed-opf / missing-container / missing-spine / missing-ncx / broken-xhtml / minimal-epub3-nav),断言 partial+warnings 逐类行为。
- **解析层表驱动**:href 归一矩阵(`./`、`../`、%XX、`#fragment`)、DRM 预检三路(白名单放行/真 DRM 拒/无 encryption 直通)、TOC 双路选择。
- **阅读器按键泵**:跨章翻页、续读位换宽不丢(golden 钉锚点)、书签/搜索跳转/Esc 层退。
- 行宽守卫 `assertLinesFit` 覆盖新屏;i18n 断言组合成品串 zh+en(CODING_STANDARDS 两条)。
- **搜索**:文本投影一致性被显式断言钉住(渲染含降级标记时的投影行为)。

## Out of Scope

- FTS5 表与全局跨源搜索(Q7 延后至立项;ADR #5「SQLite 有 FTS5 可用」不受伤害,只是 M6 不动用)。
- 终端图像渲染(sixel/kitty)、mobi/azw3、批注导出、进度云同步(PLAN §1 既有边界)。
- TUI 内书库管理(删除/重命名/移动)——删文件即下架。
- EPUB3 media overlays(音频书)、fixed-layout 版面还原(固定版式书按线性流读)。
- 阅读主题/字号独立于全局(复用全局主题)。

## Further Notes

- **上游引用**:`research-epub-libs.md`(双库实证+达标表+goread 772 行参照);`probe/` 可复跑(嵌套 go.mod,需 replace 指向本地克隆或网络)。
- **已知灰区**(票里以 fixture 钉):gbk OPF 罕见未列入;EPUB3 nav 嵌套/landmarks 误选已定过滤方案;NCX playOrder 乱序以实测样本为准。
- **真机验收输入(待用户)**:①验收真书(中文技术书,书名/路径);②真实书库目录路径。
- **里程碑**:PLAN M6;粗排四票:①解析层+fixture → ②书库+书架屏 → ③阅读器 → ④搜索+验收收口;估 2–3 周。
