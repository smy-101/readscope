# M6 EPUB 解析库评估:billdaws/epub vs ABFCode/Spine(+ 自写薄层参照)

> 研究日期:2026-11-18。方法:克隆三仓库逐行读源码、跑通全量测试、用合成脏书/DRM 样本做行为探针(go1.27.3 linux/amd64)。所有论断附可回溯来源;探针源码与原始输出归档于本目录 `probe/`。
> 评估对象 commit:billdaws/epub @ `7d7ccf3`(= v1.0.2,main)、ABFCode/Spine @ `944c4c3`(= v0.3.1,main)、Ray-D-Song/goread @ `7c25bdb`(main)。

## TL;DR

- **两库的只读 API 面都够用**(spine/manifest/按 href 取原始字节/metadata/NCX+Nav 双 TOC/封面),且都纯 Go 无 cgo、都能在本机构建并全量测试通过。
- **ABFCode/Spine 的容错模型与 readscope「partial data + warnings 照常开书」需求逐字吻合**(实证:OPF 纯垃圾也能开书,降级出 manifest+spine+生成 TOC+5 条 warnings),**但许可证是 GPL-3.0**,与 readscope 的 MIT 分发不兼容——按当前项目定位这是一票否决项,除非换依赖许可策略或绕开。
- **billdaws/epub 是 MIT + 零依赖 + 类型化错误**,API 恰好覆盖 readscope 需求,`Validate()` 天然可当 warnings 列表用;但**脏书是硬失败集**(OPF 非良构 / 缺 version / 命名空间不对 → 拒开),且实证发现**真实世界常见脏 href(`./chapter1.xhtml`)会让章节字节读取直接失败**(不做 path.Clean)。
- **DRM 两库都不检出**:有 `META-INF/encryption.xml` + 内容加密的书,两库都静默返回乱码字节(实证)。无论选哪条路,readscope 都需自补一个 ~20 行的 encryption.xml 预检(区分字体混淆与真 DRM)。
- **两个库都是 2026 年诞生、0 star、0 issue、单作者的"无社区"库**(billdaws 主体一天写完;Spine 三天发四版)。"上游死掉能 fork 自救"在代码量层面成立(分别约 1155/3361 行非测试代码),在生态层面不存在。
- **自写薄层有 goread 背书**:其 EPUB 数据面仅 772 行(MIT),支撑一个能用的终端阅读器;readscope 的 PLAN 估计「一两天」量级成立。其它维护中的第三方库(唯一活跃且正经的是 readium/go-toolkit)体量/模型错配,不构成第三候选。

---

## 1. 候选 A:billdaws/epub

仓库:https://github.com/billdaws/epub · 评估 commit `7d7ccf3` = tag v1.0.2 = main tip。

### 1.1 许可证 / 模块 / 版本 / 维护状态

| 项 | 事实 | 来源 |
|---|---|---|
| 许可证 | MIT,Copyright (c) 2026 Bill Daws | [LICENSE](https://github.com/billdaws/epub/blob/7d7ccf3/LICENSE) 1-3 行 |
| 模块路径 | `github.com/billdaws/epub` | [go.mod](https://github.com/billdaws/epub/blob/7d7ccf3/go.mod) L1 |
| 最新版本 | v1.0.2(2026-05-01T18:55:35Z 发 tag;module proxy `@latest` 同 hash) | proxy.golang.org `github.com/billdaws/epub/@latest` → `{"Version":"v1.0.2","Hash":"7d7ccf3..."}`;git tag 列表 v1.0.0–v1.0.2 |
| 最近 commit | `7d7ccf3` 2026-05-01 "feat: cover handling from opf" | `git log`(本地克隆) |
| 仓库年龄/节奏 | 2026-04-06 创建;28 个 commit 中约 24 个集中在创建当天(一天写完主体),5 月补一个 cover feature 后至今(2026-11-18)无活动 | `git log --date=short`;GitHub API `created_at: 2026-04-06` |
| issue 活跃度 | **0 open / 0 closed / 0 PR(全历史)**;0 star / 0 fork | GitHub API `repos/billdaws/epub` 与 `issues?state=all`(2026-11-18 查询) |
| 判断 | 单作者、无社区、无 issue 流;README 自述 "zero dependencies" 是准确的。**死库风险高,但代码量小到可以整库 fork 进 tree** | — |

### 1.2 依赖面

- `go.mod` 只有 `module github.com/billdaws/epub` + `go 1.26.1`,**无任何 require**([go.mod](https://github.com/billdaws/epub/blob/7d7ccf3/go.mod) 全文 3 行)。纯标准库(archive/zip + encoding/xml + path/strings/sync)。
- 无 cgo。`go 1.26.1` 指令 **要求工具链 ≥1.26.1**;readscope 当前 `go 1.27.3`(readscope/go.mod L3),满足。
- 本机 `go build ./...`、`go test ./...` 全过(go1.27.3)。

### 1.3 API 面(对照 readscope 需求)

| 需求 | 支持 | 源码依据 |
|---|---|---|
| spine 顺序 | ✅ `Package.Spine []SpineItem{IDRef, Linear}` | [opf.go](https://github.com/billdaws/epub/blob/7d7ccf3/opf.go) L42-45、L227-233 |
| manifest 资源 | ✅ `Package.Manifest []Item{ID, Href, MediaType, Properties}`;href 已拼成 zip 相对路径 | opf.go L31-39、L212-225 |
| 按 href 读章节原始字节 | ✅ `Reader.ReadItem(item) ([]byte, error)`,manifest item → 原始字节;Reader 持开 fd、`sync.RWMutex` 并发安全 | [reader.go](https://github.com/billdaws/epub/blob/7d7ccf3/reader.go) L10-18、L54-75 |
| metadata(title/author/language) | ✅ Title(第一个 dc:title)/ Authors(全部 creator)/ Language(第一个)/ Identifier(unique-identifier 优先)/ PublicationDate | opf.go L23-29、L298-327 |
| 封面 | ✅ `Package.Cover *Item`:EPUB3 `properties~cover-image` + EPUB2 `<meta name="cover">` 两路径;**不解析 `<guide><reference type="cover">`** | opf.go L19、L240-261 |
| TOC NCX | ✅ `decodeNCX`:navMap>navPoint 递归 → `[]NavPoint{Title, Src(zip 相对,保留 #fragment), Children}` | [toc.go](https://github.com/billdaws/epub/blob/7d7ccf3/toc.go) L95-128 |
| TOC EPUB3 Nav | ✅ `decodeNav`:token 扫描找 `epub:type` 含 "toc" 的 `<nav>`,解析嵌套 `<ol><li><a>`;找不到 → 类型化 `MissingNavElementError` | toc.go L130-246、[errors.go](https://github.com/billdaws/epub/blob/7d7ccf3/errors.go) L42-48 |
| TOC 选择策略 | nav(properties 含 "nav")优先,NCX 兜底;两者皆无 → `MissingTOCError` | toc.go L40-58 |
| 额外 | 写 EPUB3(write.go,readscope 用不上);`Validate(pkg) []Violation` 结构校验(空 spine/断 idref/缺 title 等 13 种 code) | [validate.go](https://github.com/billdaws/epub/blob/7d7ccf3/validate.go) L11-58 |

注意:spine idref → manifest item 的 join 由调用方做(无 helper);`OpenTOC/OpenPackage` 是一次性 API,各自重新开 zip。

### 1.4 容错(脏书行为,实证)

源码行为 + 本地探针(`.scratch/m6-epub/probe/output.txt`,样本构造见 `probe/main.go`):

| 脏书样本 | 实际行为 | 依据 |
|---|---|---|
| 缺 NCX 的 EPUB2 | `Open` **成功**(spine/manifest 可用);`OpenTOC` 返回类型化 `*MissingTOCError`;`Validate` 报 1 条 violation → 调用方可组合出 partial+提示 | 探针输出;opf.go(缺 NCX 不影响 parsePackage)、toc.go L56-58 |
| OPF 缺 `version` 属性 | **硬失败** `UnsupportedVersionError` | opf.go L187-202;探针输出 |
| OPF 非良构(垃圾文本) | **硬失败**(xml decode error) | opf.go L131-139;探针输出 |
| OPF 命名空间缺失/不对 | **硬失败**(`xmlPackage` 的 XMLName 绑定 `http://www.idpf.org/2007/opf`) | opf.go L91-92;探针输出 |
| 损毁 zip | **硬失败** `zip: not a valid zip file` | reader.go L23-26;探针输出 |
| OPF 声明 iso-8859-1 | ✅ 专门处理:自写 latin1→UTF-8 CharsetReader,title 正确解出 "Café Test" | opf.go L141-163;探针输出 |
| manifest href 写 `./chapter1.xhtml` | **章节字节读取失败**:href 原样拼成 `OEBPS/./chapter1.xhtml`,`ReadItem` → `ItemNotFoundError`(不做 path.Clean、不剥 `./`;全库无 path.Clean/TrimPrefix/Unescape,grep 为证) | opf.go L212-218、epub.go L87-95;探针 `dotprobe-output.txt`(billdaws ReadItem err vs Spine 成功) |
| 结论 | 「partial + warnings」**部分可达**:Open 只依赖 container+OPF,TOC/校验失败可独立降级;但 **OPF 本身非良构/缺 version/异 ns/脏 href 一律拒开或读不到正文**。对"扫描整个书库目录、啥书都要能上架"的场景是实打实的风险 | — |

### 1.5 DRM 行为(实证)

- 全库 **无任何 encryption.xml 处理**(grep `encryption|drm` 无命中,仅 errors/toc 等)。
- 合成 ADEPT 样式样本(有 `META-INF/encryption.xml`,chapter1 内容替换为加密乱码):`Open` 成功、`OpenTOC` 成功、`ReadItem` **静默返回 168 字节乱码,零报错**(probe/output.txt `drm-encryption` 段)。
- zip 条目级密码加密(`zip -P`)样本:`Open` 失败于读 container.xml 的 `flate: corrupt input`(zipprobe-output.txt)——Go stdlib 对加密条目 `Open()` 成功、读取时报 flate 错,该错误会向上冒泡,但语义是"corrupt input"而非"DRM"。
- **判定:不达标,需调用方自补预检**(见 §6 实现指引)。

### 1.6 测试与示例

- 测试文件:`epub_test.go`、`opf_test.go`(462 行)、`reader_test.go`(263 行,含并发安全测试)、`toc_test.go`(270 行)、`validate_test.go`(206 行)、`write_test.go`(246 行);合计约 1560 行测试 vs 1155 行库代码(其中读路径相关约 975 行)。
- 真书数据:`testdata/` 5 本 Gutenberg EPUB(gift-of-the-magi 等)。
- README 有 reading/writing/validation 三段用法示例;CI:`.github/workflows/ci.yml`(test job)。
- 本机 `go test ./...` 全绿。

---

## 2. 候选 B:ABFCode/Spine

仓库:https://github.com/ABFCode/Spine · 评估 commit `944c4c3` = tag v0.3.1 = main tip。

### 2.1 许可证 / 模块 / 版本 / 维护状态

| 项 | 事实 | 来源 |
|---|---|---|
| **许可证** | **GPL-3.0**(LICENSE 为完整 GPL v3 文本,643 行,"Version 3, 29 June 2007";GitHub API 因此报 `NOASSERTION`) | [LICENSE](https://github.com/ABFCode/Spine/blob/944c4c3/LICENSE) L1-2;API license 字段 |
| 模块路径 | `github.com/ABFCode/Spine` | [go.mod](https://github.com/ABFCode/Spine/blob/944c4c3/go.mod) L1 |
| 最新版本 | v0.3.1(2026-01-26;proxy `@latest` 同 hash `944c4c3`) | proxy.golang.org `github.com/!a!b!f!code/!spine/@latest`;git tag v0.1.0–v0.3.1 |
| 最近 commit | `944c4c3` 2026-01-25/26 "Improve GoDoc and split API docs";此后至 2026-11-18 无活动 | `git log`;API `pushed_at: 2026-01-26` |
| 仓库年龄/节奏 | 2026-01-24 创建,**三天内 11 commits 发完 v0.1.0→v0.3.1 四个 release**,之后停更 | `git log`;GitHub releases API |
| issue 活跃度 | **0 open / 0 closed / 0 PR;0 star / 0 fork** | GitHub API(2026-11-18) |
| 判断 | 单作者、无社区、已停更 10 个月。README 自我定位 "Fast, tolerant EPUB parser",文档质量意外地高(docs/API.md、errors.md、errors.jsonl、CHANGELOG) | — |

**GPL-3.0 对 readscope 的含义**:Go 静态链接会把 Spine 编进 readscope 二进制,分发即要求整体以 GPL-3 兼容方式授权;readscope 是 MIT(readscope/LICENSE),**两者不兼容**。除非 readscope 改许可证或向作者取商业/例外授权,此依赖不可用。以下仍完整评估其技术面,因为它同时是「自写容错层该抄哪些行为」的最佳行为规格参照。

### 2.2 依赖面

- go.mod:`golang.org/x/net v0.49.0` 唯一依赖;go.sum 仅 x/net 两条(go.sum 全文 2 行)。`go 1.25.0`。无 cgo。
- x/net 只用 `html` 子包(Tokenizer 解析 Nav/XHTML)——readscope 渲染管线已依赖 x/net,零新增依赖种类。

### 2.3 API 面

| 需求 | 支持 | 源码依据 |
|---|---|---|
| spine 顺序 | ✅ `Book.Spine []SpineItem{IDRef, Href(已解析), Linear, Properties}` | [types.go](https://github.com/ABFCode/Spine/blob/944c4c3/types.go) L76-82;parse.go L119-133(idref→href join) |
| manifest | ✅ `Book.Manifest map[ID]ManifestItem`(另有 `ManifestItems()` 排序视图) | types.go L67-74;[book.go](https://github.com/ABFCode/Spine/blob/944c4c3/book.go) L90-100 |
| 按 href 原始字节 | ✅ `Book.OpenResource(href) (io.ReadCloser, error)`——任意 zip 路径(自动剥 #fragment + path.Clean);**另有** `Blocks/BlocksByHref/Chapters/ForEachChapter/Chunks` 文本抽取层(readscope 可不用) | [content.go](https://github.com/ABFCode/Spine/blob/944c4c3/content.go) L10-22;[chapters.go](https://github.com/ABFCode/Spine/blob/944c4c3/chapters.go) L10-97;[chunk.go](https://github.com/ABFCode/Spine/blob/944c4c3/chunk.go) L9-26 |
| metadata | ✅ 富模型:Title/Titles(带 refines)、Authors/Creators、Contributors、Language(s)、Identifiers(scheme)、Publisher、PubDate、**Series/SeriesIndex(belongs-to-collection)**、Subjects、Rights、Descriptions、Modified、Collections、**CoverHref**;`Cover()/OpenCover()` 直接给封面字节+Content-Type | types.go L4-25;opf.go L55-326(meta/refinements/series);content.go L24-60 |
| TOC NCX | ✅ `parseNCX` navMap 递归 → `TOCItem{Label, Href, Children, Target}` | [toc.go](https://github.com/ABFCode/Spine/blob/944c4c3/toc.go) L76-121 |
| TOC EPUB3 Nav | ✅ `parseNav` 用 x/net/html Tokenizer,解析嵌套 ol;**同时**抽 landmarks 与 page-list | toc.go L123-265;L34-38 |
| TOC 选择策略 | `spine@toc` 属性 → nav property → NCX(media-type 或 .ncx 后缀)。⚠️ 与 billdaws 相反:**EPUB3 兼容书(同时留 spine@toc=ncx)会优先走 NCX** | toc.go L13-32 |
| TOC 无 | 默认 `Fallbacks.GenerateTOC` 从 XHTML 标题生成(带 warning) | [fallback.go](https://github.com/ABFCode/Spine/blob/944c4c3/fallback.go) L116-169 |
| 输入形态 | `ParseFile/ParseBytes/Parse(io.Reader)(>32MiB 落盘 spool)/Open(ReaderAt)` + 可复用 `Parser` + Option 链 | [parser.go](https://github.com/ABFCode/Spine/blob/944c4c3/parser.go) L29-131;[options.go](https://github.com/ABFCode/Spine/blob/944c4c3/options.go) L104-191 |
| ⚠️ 并发 | **Book 非并发安全**(文档明示,需外部同步)——对比 billdaws Reader 自带锁;影响 M6 后台 FTS 索引与 UI 并行读 | book.go L12-13 |

### 2.4 容错(脏书行为,实证)

设计就是 partial data + warnings:`Book.Warnings []Warning{Code, Message, Path}` + WarningHandler 回调(book.go L183-189;types.go L112-117);8 个 sentinel 错误可 `errors.Is`(errors.go L5-22)。

| 脏书样本 | 实际行为 | 依据 |
|---|---|---|
| OPF 缺 `version` | **照常开书** + 1 warning("missing package version"),元数据/spine/TOC 全在 | 探针输出;parse.go L186-188 |
| OPF 纯垃圾(非 XML) | **照常开书**:`ErrMalformedOPF` 降级为 warning → 扫描 zip 推断 manifest+spine → 从标题生成 TOC → 共 5 条 warnings,正文 `OpenResource` 正常读 | 探针输出;parse.go L100-105、L135-154;fallback.go L62-109 |
| OPF 命名空间缺失 | **照常解析**(token 级 localName 匹配,无视 ns) | opf.go L328-333;探针输出 |
| 缺 container.xml | ScanArchive 兜底:zip 里找 `*.opf`;golden fixture 证实开书成功 | parse.go L46-55;container.go L56-64;testdata/expected/fixture-missing-container.epub.json |
| 缺 spine | InferSpine 从 manifest 推断(golden fixture 在库) | parse.go L145-154;fixture-missing-spine golden |
| 损毁 zip | 硬失败 `ErrInvalidZip`(返回 nil book)——这是唯一不可挽救类 | parse.go L32-35;探针输出 |
| OPF 声明 iso-8859-1 | **OPF 解码整体失败**(xml.Decoder 无 CharsetReader,Strict=false 也不豁免 charset)→ 降级 fallback,**全部元数据丢失**(title 变成从首标题猜的 "Ch1")——宽容库反而输给 billdaws 的一处 | 探针 `opf-latin1` 段;opf.go L18-24 |
| href `./x.xhtml` | ✅ `resolvePath` path.Clean 归一,读取成功 | util.go L12-41;探针 `dotprobe-output.txt` |
| 配置语义 | 默认(DefaultConfig)四类 fallback 全开 + Strict=false;`WithStrict(true)` 关 fallback 换硬错误 | options.go L44-63、L85-97 |
| 返回值语义 | 非 Strict 下多数错误降级为 warning、`err == nil`;部分路径返回 `(book, err)` 同时非 nil——调用方须两边都看(README 首例注释即如此教) | parse.go L53、L81、L89、L103、L152;[README](https://github.com/ABFCode/Spine/blob/944c4c3/README.md) Quick start 注释 |

### 2.5 DRM 行为(实证)

- 全库无 encryption 处理(grep 无命中)。
- ADEPT 样式样本:`ParseFile` 成功、**0 warnings**、`OpenResource` 静默返回乱码字节(probe/output.txt)。
- zip 密码条目样本:book 能"打开"(8 条 warnings,内容全降级),但正文读取时 flate corrupt 错误(zipprobe-output.txt)。
- **判定:不达标**——甚至比 billdaws 更"宽容到危险"(乱码开书无任何提示)。

### 2.6 测试与示例

- 测试:`spine_test.go`、`compat_test.go`(**golden 兼容性测试**:`fixture-broken-xhtml / malformed-opf / minimal-epub2 / minimal-epub3-nav / missing-container / missing-spine` 五种坏书,断言 JSON 含 `warnings` 字段)、`chapters_test.go`、`examples_test.go`、`benchmark_test.go`。
- 工具链:`cmd/spine-fixtures`(合成坏书)、`cmd/spine-golden`、`cmd/spine-fetch-gutenberg`、`cmd/spine-fetch-tests`(W3C/EPUBCheck 样本拉取,external 目录 gitignore);docs/API.md、docs/errors.md + errors.jsonl(机器可读错误表)。
- 非测试代码约 3361 行(15 个 .go)。本机 `go test ./...` 全绿。

---

## 3. 实证探针汇总(两库并排)

探针:`.scratch/m6-epub/probe/`(main.go 九样本 + dotprobe + zipprobe;fixture 全部合成,构造逻辑内嵌源码)。输出逐样本对照:

| 样本 | billdaws/epub v1.0.2 | ABFCode/Spine v0.3.1 |
|---|---|---|
| 规范 EPUB2 / EPUB3 | 全部正常(TOC/metadata/正文) | 全部正常 |
| EPUB2 缺 NCX | Open ✅;OpenTOC → `MissingTOCError`;Validate 1 violation | 开书 ✅ + warning,从标题生成 TOC |
| **DRM(encryption.xml+密文)** | **开书 ✅,读正文=静默乱码** | **开书 ✅ 0 警告,读正文=静默乱码** |
| OPF 缺 version | ❌ 拒开 | ✅ + 1 warning |
| OPF 垃圾文本 | ❌ 拒开 | ✅ + 5 warnings(降级推断) |
| OPF 无 ns | ❌ 拒开 | ✅ 正常 |
| OPF iso-8859-1 | ✅ 正确解码("Café Test") | ❌ 元数据全丢(降级) |
| href 带 `./` | ❌ ReadItem 找不到条目 | ✅ 正常读取 |
| 损毁 zip | ❌ 硬失败 | ❌ 硬失败(`ErrInvalidZip`) |
| zip 密码条目 | ❌ Open 失败(flate corrupt) | ⚠️ 开书但正文读取报错 |

---

## 4. 其它维护中的 Go EPUB 只读解析库(快速扫描)

GitHub 搜索 `epub language:go` 按 star 排序(2026-11-18,API 单页 30 条)+ 人工甄别「解析 vs 生成/工具/阅读器」:

- **readium/go-toolkit**(BSD-3-Clause,72★,develop @ `1b736f88d29b` 2026-11-17,Readium 官方,活跃):`pkg/parser/epub/` 是正经全量解析器——`parser_packagedoc.go`、`parser_ncx.go`、`parser_navdoc.go`、**`parser_encryption.go`(encryption.xml 处理)与 `deobfuscator.go`(IDPF/Adobe 字体去混淆,即正确区分"字体混淆≠DRM"的标准实现)**。但 go.mod 依赖 gorilla/mux、negroni、viper、cobra、pdfcpu、logrus 等一大串(pkg/streamer 服务端模型,Web Publication/fetcher 抽象),**API 形态是"服务器/流式发布"而非"开文件读字节"**,为终端阅读器引入整个 readium 模型属重量级错配。不入围,但 §6 的 DRM 预检可抄它的判定口径。
- **pgaskin/kepubify**(MIT,pushed 2026-11-10):EPUB→Kobo 转换工具,内含解析逻辑,但是工具仓库、无稳定库 API 承诺,不入围。
- **taylorskalyo/goreader**(MIT,103★,last push 2025-03-14):老牌终端 EPUB 阅读器,其 `epub` 子包曾是社区常用解析参考;已约 20 个月无提交、EPUB3 覆盖弱,不入围。
- **kapmahc/epub**(MIT,2016 年停更):死库。
- **bmaupin/go-epub**(MIT,222★):**生成** EPUB,非解析,明确排除(任务书点名要区分的类别)。其余高分条目(kepubify/bookhunter/kaf-cli/FictionDown 等)均为下载/转换/阅读器应用。
- **结论:不存在第三个「维护中 + MIT + 薄解析」候选。** 活跃维护且正规的只有 readium/go-toolkit(重)。

---

## 5. 参照物:goread 的自写 EPUB 层(MIT)

goread(@ `7c25bdb`,MIT,227★,pushed 2026-06-25)的 EPUB 数据面在 **pkg/epub/**(注意:任务书提的 pkg/parser 是 HTML→tview 样式文本的渲染层,对应 readscope 自有的 render 管线,不在对照范围):

- **体量:`epub.go` 723 行 + `meta.go` 49 行 = 772 行**,依赖仅 archive/zip + encoding/xml + google/uuid(TOC 节点 ID,readscope 不需要)+ 内部 DList。这就是"自写薄层"的实测成本上限——它还包含了 UI 树模型;一个纯数据面(容错+warnings+DRM 预检)估计 **400–700 行**。PLAN 的「一两天」估计成立。
- zip:直接 `zip.OpenReader` + `Reader.Open(name)`(Go 1.16+ API)(epub.go L146、L181)。
- container.xml:一次 `xml.Decode`,**只认第一个 rootfile**;带真实世界 hack——container 指向的路径开不了就盲试 `OEBPS/` 前缀(epub.go L197、L203-227)。
- OPF:一次 Decode;metadata 用 `metadata>*` 泛型条目 + Local 名 switch,天然免疫命名空间(meta.go L23-48);多 dc:title 时后值覆盖前值(meta.go L27-28)。**无 charset 处理**。
- TOC:**NCX 优先**(注释:"works for both EPUB 2.0 and some EPUB 3.0",epub.go L253-265);仅当 version=="3.0" 且无 NCX 才找 `properties="nav"`(L267-281);Nav 解析是 `body>nav>ol>li>a` 的扁平单层结构体,明注 "we don't handle nested nav links in EPUB 3.0"(L126-137、L399),且不看 `epub:type`(首个 nav 可能是 landmarks)。
- 容错策略:**container/OPF 失败即拒开**(NewEpub 硬失败链,L156-175);TOC 打不开/解不动 → DebugLog 后**用 spine+manifest 补出章节列表**(IsShadow 标记不在官方 TOC 的项,title 回退 idref)(L316-533)——即"TOC 层宽容、结构层严格"。
- DRM:无任何 encryption 感知(grep 全 pkg/cmd 无命中)。
- 无封面 API(阅读器不需要)。

**参照结论**:一个活跃的真终端阅读器,其 EPUB 数据面就是几百行 stdlib 代码 + 若干真实世界补丁(./ 剥离、OEBPS 盲试)。它同时示范了自写层最容易欠账的地方:EPUB3 nav 嵌套/epub:type、charset、DRM——这些恰好是 readscope 需求清单上的项。

---

## 6. 对照 readscope 需求的达标表

需求取自 PLAN(ADR #3/#6、技术选型表「EPUB 解析」行)与任务书。✅ 达标 / ⚠️ 部分 / ❌ 不达标 / ? 未深查。

| readscope 需求 | billdaws/epub v1.0.2(MIT) | ABFCode/Spine v0.3.1 | goread 层(自写参照) | readium/go-toolkit |
|---|---|---|---|---|
| 只读解析:spine 顺序 | ✅ opf.go L227-233 | ✅ types.go L76-82 | ✅ | ✅ |
| manifest 资源 | ✅ opf.go L31-39 | ✅ | ✅ | ✅ |
| 按 href 取资源原始字节 | ⚠️ ReadItem 逐 item 可用;但 `./`、未归一路径失败(实证 dotprobe) | ✅ OpenResource 任意路径+Clean | ✅(带 hack) | ✅(fetcher 抽象) |
| metadata title/author/language | ✅ opf.go L298-327 | ✅ 富模型 | ✅ | ✅ |
| 封面 | ✅ EPUB2/3 双路径;无 guide | ✅ + 字节级 `Cover()` | ❌ 无 | ✅ |
| TOC:NCX 与 Nav 双支持 | ✅ nav 优先/NCX 兜底(toc.go L40-58) | ✅(+landmarks/pagelist;但 spine@toc 会先于 nav) | ⚠️ NCX 优先,nav 扁平且可能选错 nav | ✅ |
| 脏书:partial + warnings 照常开书 | ⚠️ 缺 NCX/缺字段可降级;**OPF 非良构/缺 version/异 ns → 拒开** | ✅ 设计即如此(实证:OPF 垃圾也开书) | ⚠️ 仅 TOC 层降级 | ? 未深查 |
| DRM:明确检出以便报「不支持」 | ❌ 静默乱码(实证) | ❌ 静默乱码(实证) | ❌ | ✅ parser_encryption.go + deobfuscator.go |
| 无 cgo | ✅ 零依赖 | ✅ 仅 x/net(readscope 已有) | ✅ | ✅(纯 Go,但依赖树重) |
| 许可证兼容 MIT | ✅ MIT | ❌ **GPL-3.0,链接即传染** | ✅ MIT | ✅ BSD-3 |
| 长期维护/可 fork 自救 | ⚠️ 0 社区、一天写成、5 月起静默;1155 行可整库 vendor/fork | ⚠️ 0 社区、三天写成、已停更 10 个月;3361 行;GPL fork 仍 GPL | ✅ 227★ 活跃(但层不独立成库,得抄) | ✅ 组织级维护 |
| 工具链兼容 | ✅ go 1.26.1 ≤ readscope 1.27.3 | ✅ go 1.25.0 | ✅ | ✅ |
| 并发(后台索引 vs UI 读) | ✅ Reader 自带锁 | ❌ Book 非并发安全(book.go L13) | n/a | ? |

## 7. 「用库 vs 自写」决策所需的事实(两面摆开,不做裁决)

**用 billdaws/epub 的理由**:MIT、零依赖、API 与 readscope 需求几乎一一对应、类型化错误+Validate 可拼出 warnings 语义、latin1 OPF 有独门处理、Reader 并发安全、测试/CI 齐整;死库也只需 vendor ~1000 行。
**用它的代价**:①硬失败集要自己包一层(OPF 非良构/缺 version/异 ns → 目前直接拒开;若要"照常开书"需 fork 改 `decodePackage`);②href 归一化缺失(`./`、Clean、%XX)要自己补(实证已踩);③DRM 预检要自写;④上游零社区,所有修复都归自己。

**用 ABFCode/Spine 的理由**:容错/降级/警告模型就是 readscope 想要的成品(golden 测试连坏书都锁了行为),还有 landmarks/series/封面字节/分块(可顺手喂 FTS5)等超出需求的面。
**用它的代价**:①**GPL-3.0 与 MIT 分发冲突,此项在当前项目定位下接近一票否决**;②latin1 OPB 元数据全丢、TOC 选择策略反直觉(spine@toc 优先)、Book 非并发安全;③同样零社区且已停更;④DRM 同样静默乱码。

**自写薄层的理由**:goread 实证 772 行即可支撑终端阅读器;readscope 只需数据面(400–700 行),可以精确实现「partial+warnings+DRM 检出」而不背任何许可/上游风险;Spine 的 fallback 语义 + golden 坏书清单(5 类)+ readium 的 encryption 判定口径都是现成的行为规格,照抄行为不抄代码。
**自写的代价**:charset(latin1/gbk OPF)、href 归一化、EPUB3 nav 嵌套/epub:type、NCX playOrder 边角这些"真实世界的灰"要自己一个个踩——这正是两库作者已经替你踩过的部分;测试矩阵(至少 goread/billdaws 级别的 fixture 集)也要自建。

**无论哪条路都要做的**(两库均不提供):DRM 预检——开书前 `zip` 里查 `META-INF/encryption.xml`,解析后按算法白名单放行(仅 `http://www.idpf.org/2008/obfuscation` / `http://www.adobe.com/pdf/enc#RC` 属字体混淆,其余视为 DRM 报"不支持");此判定口径即 readium/go-toolkit `parser_encryption.go` + `deobfuscator.go` 的分工。

---

## 8. 引用清单

**仓库与版本**
- billdaws/epub @ `7d7ccf3`(= v1.0.2, 2026-05-01):https://github.com/billdaws/epub/tree/7d7ccf3 · proxy:https://proxy.golang.org/github.com/billdaws/epub/@latest
- ABFCode/Spine @ `944c4c3`(= v0.3.1, 2026-01-26):https://github.com/ABFCode/Spine/tree/944c4c3 · proxy:https://proxy.golang.org/github.com/!a!b!f!code/!spine/@latest
- Ray-D-Song/goread @ `7c25bdb`(2026-06-25):https://github.com/Ray-D-Song/goread/tree/7c25bdb(pkg/epub/)
- readium/go-toolkit develop @ `1b736f88d29b`(2026-11-17):https://github.com/readium/go-toolkit/tree/develop(pkg/parser/epub/)
- GitHub REST API(2026-11-18 查询):repos/{billdaws/epub, ABFCode/Spine, Ray-D-Song/goread, readium/go-toolkit}、issues?state=all、search/repositories?q=epub+language:go

**关键文件(行号基于上述 commit)**
- billdaws:go.mod / LICENSE / opf.go(14-45, 91-163, 187-327)/ reader.go(10-75)/ toc.go(40-246)/ errors.go / validate.go(11-153)/ epub.go(34-95)/ testdata/(5 本 Gutenberg)/ .github/workflows/ci.yml
- Spine:go.mod / LICENSE(GPL-3.0)/ parse.go(31-190)/ opf.go(18-326)/ parser.go / book.go(12-203)/ content.go(10-95)/ toc.go(13-331)/ fallback.go / options.go(44-191)/ types.go / errors.go / docs/(API.md, errors.md, errors.jsonl)/ testdata/expected/*.json(warnings 字段)
- goread:pkg/epub/epub.go(140-533, 591-702)/ pkg/epub/meta.go / go.mod
- go-toolkit:pkg/parser/epub/(parser_encryption.go, deobfuscator.go, parser_ncx.go, parser_navdoc.go, parser_packagedoc.go)/ go.mod
- readscope:docs/PLAN.md(L60 技术选型、L213 M6、L223)/ go.mod(go 1.27.3)/ LICENSE(MIT)

**本地实证产物(可复跑)**
- `.scratch/m6-epub/probe/main.go`(九样本构造+双库探针)、`output.txt`(结果)
- `.scratch/m6-epub/probe/dotprobe.go` + `dotprobe-output.txt`(`./` href 差异)
- `.scratch/m6-epub/probe/zipprobe.go` + `zipprobe-output.txt`(zip 密码条目行为)
- 复跑方式:`go run .`(需将 go.mod 的 replace 指向本地克隆或恢复网络取模块);本机验证环境 go1.27.3 linux/amd64,2026-11-18。
