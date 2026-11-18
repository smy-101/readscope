# 01: EPUB 解析薄层 + 合成/坏书 fixture

**What to build:** `internal/epub` 自写薄层(ADR #17,~400–700 行):`Open(path) → Book`(title/authors/language、spine 顺序、manifest、TOC、类型化 warnings)+ `ReadItem(href)` 原始字节。唯二硬失败 = 损坏 zip、真 DRM(encryption.xml 预检,字体混淆算法白名单放行);其余脏书 partial+warnings 照常开书(缺 container → zip 扫 `*.opf`;OPF 非良构/缺 version/缺 ns → manifest/spine 从 zip 推断、TOC 从章节标题生成)。href 归一(path.Clean/#fragment/%XX)、latin1 CharsetReader、TOC 双路(EPUB3 nav 优先,`epub:type=toc` 过滤防 landmarks 误选,NCX 兜底)。纯 Go 的合成 EPUB fixture 生成器进 testdata(中文禁则/代码块/图片/嵌套列表/多级 TOC/latin1 OPF)+ 坏书 6 类对照集;行为规格抄 Spine 语义(不抄代码,GPL 不传染)、goread 真实世界 hack、readium DRM 判定口径,全部见 spec Implementation Decisions。

**Blocked by:** 无(可立即开始)。

**Status:** done

- [x] Open 规范 EPUB2/EPUB3 → metadata(title/authors/language)、spine 顺序、manifest、NCX/Nav 双 TOC 全部正确(合成 fixture 恒测)
- [x] 坏书对照集逐类断言:malformed-opf / missing-container / missing-spine / missing-ncx / broken-xhtml / minimal-epub3-nav——全部开书成功且 warnings 如数(broken-xhtml 正文读取报错但不毁书)
- [x] 损坏 zip → 类型化硬失败;zip 密码条目行为有断言(开书或拒开,行为钉住即可)
- [x] DRM 预检三路:encryption.xml 白名单字体混淆 → 放行照常读;其它算法 → `ErrDRMUnsupported`;无 encryption.xml → 直通(合成样本断言)
- [x] href 归一矩阵:`./` `../` %XX `#fragment`、spine idref→manifest join,全部 ReadItem 成功(探针实证 billdaws 栽点)
- [x] latin1 OPF 元数据正确解出(CharsetReader)
- [x] TOC 选择:nav(`epub:type=toc`)优先于 NCX;首个 nav 是 landmarks 时不错选;皆无 → 标题生成 + warning
- [x] Warnings 类型化可编程消费(码+消息),书架屏/doctor 下游可用
- [x] 零新依赖(archive/zip + encoding/xml + 既有 x/net/html);CI 恒离线

## Comments

**实现落点**:internal/epub(epub.go 本体)+ internal/epub/fixture(纯 Go 合成生成器,测试时生成到 t.TempDir,零二进制进 git)。切片 1–8 逐红绿提交(e149db1 起)。

**用户拍板的四处偏差(2026-11-19)**:
1. 票面「生成器进 testdata」→ Go 工具链忽略 testdata 目录(不可导入),改 importable `internal/epub/fixture` 包 + TempDir 生成。生成器进 git 即「golden in git」,CI 零生成步骤。
2. broken-xhtml 样本 = zip 条目压缩数据损坏(CreateRaw 写垃圾 deflate):ReadItem 报错、书不毁——错误落在本层缝上(原始字节层不解析 XHTML,畸形 HTML 归票 03 渲染管线)。
3. 票面「OPF 非良构/缺 version/缺 ns → zip 推断」与探针实证矛盾(Spine 实证:缺 version 照常开书+警告、缺 ns 照常解析)。按实证钉容忍解析(缺 version/缺 ns 零警告照常解析,fixture `OPFNoVersionNoNs` 钉住)——忘写 version 的书丢全部元数据才是不合理。
4. malformed-opf 降级时 zip 推断把 toc.ncx 收进 manifest,NCX 兜底命中(层级 TOC),优于票面的「标题生成」;标题生成仅当无任何 TOC 源。已批准钉住。

**双轴评审(2026-11-19,workflow 并行子代理,diff = 923a331...HEAD)**:Standards 轴 2 硬项已修(三处 path.Dir 同形块提炼 `zipDir`;`open` helper 注释声明漂移)+ 拍尘 3 项(死字段/手写 equal→slices.Equal/守卫顺序);Spec 轴补齐:encryption.xml 无/垃圾两路具名断言、NoOPF 全推断样本、untyped-nav 选择钉住。评审另逮两真 bug 已修:inferFromZip map 随机序→zip 目录序(names 切片);NCX 找到但不可用时 missing-toc 警告重复。修复后全量 go test ./... 绿,gofmt/vet 净,go.mod/go.sum 零改动。

**其它钉住的裁量**:缺 container 的 NoOPF 样本出双警告 [missing-container, missing-opf](两个事实对 doctor 都有用);encryption.xml 非良构 → 保守拒开 ErrDRMUnsupported(分不清加密了什么就不放行);未标型 nav(无 epub:type)次选于 toc 型、压过 NCX(landmarks 带 type 不会被选中)。
