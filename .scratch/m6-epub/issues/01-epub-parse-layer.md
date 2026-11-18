# 01: EPUB 解析薄层 + 合成/坏书 fixture

**What to build:** `internal/epub` 自写薄层(ADR #17,~400–700 行):`Open(path) → Book`(title/authors/language、spine 顺序、manifest、TOC、类型化 warnings)+ `ReadItem(href)` 原始字节。唯二硬失败 = 损坏 zip、真 DRM(encryption.xml 预检,字体混淆算法白名单放行);其余脏书 partial+warnings 照常开书(缺 container → zip 扫 `*.opf`;OPF 非良构/缺 version/缺 ns → manifest/spine 从 zip 推断、TOC 从章节标题生成)。href 归一(path.Clean/#fragment/%XX)、latin1 CharsetReader、TOC 双路(EPUB3 nav 优先,`epub:type=toc` 过滤防 landmarks 误选,NCX 兜底)。纯 Go 的合成 EPUB fixture 生成器进 testdata(中文禁则/代码块/图片/嵌套列表/多级 TOC/latin1 OPF)+ 坏书 6 类对照集;行为规格抄 Spine 语义(不抄代码,GPL 不传染)、goread 真实世界 hack、readium DRM 判定口径,全部见 spec Implementation Decisions。

**Blocked by:** 无(可立即开始)。

**Status:** pending

- [ ] Open 规范 EPUB2/EPUB3 → metadata(title/authors/language)、spine 顺序、manifest、NCX/Nav 双 TOC 全部正确(合成 fixture 恒测)
- [ ] 坏书对照集逐类断言:malformed-opf / missing-container / missing-spine / missing-ncx / broken-xhtml / minimal-epub3-nav——全部开书成功且 warnings 如数(broken-xhtml 正文读取报错但不毁书)
- [ ] 损坏 zip → 类型化硬失败;zip 密码条目行为有断言(开书或拒开,行为钉住即可)
- [ ] DRM 预检三路:encryption.xml 白名单字体混淆 → 放行照常读;其它算法 → `ErrDRMUnsupported`;无 encryption.xml → 直通(合成样本断言)
- [ ] href 归一矩阵:`./` `../` %XX `#fragment`、spine idref→manifest join,全部 ReadItem 成功(探针实证 billdaws 栽点)
- [ ] latin1 OPF 元数据正确解出(CharsetReader)
- [ ] TOC 选择:nav(`epub:type=toc`)优先于 NCX;首个 nav 是 landmarks 时不错选;皆无 → 标题生成 + warning
- [ ] Warnings 类型化可编程消费(码+消息),书架屏/doctor 下游可用
- [ ] 零新依赖(archive/zip + encoding/xml + 既有 x/net/html);CI 恒离线
