# 02: 书库扫描 + 书架屏

**What to build:** `internal/library` 递归扫描书库目录(`*.epub`):books upsert(path 唯一,元数据+warnings 走 01 解析层),盘上消失的行清除 = 下架(progress/bookmarks 随 CASCADE);`readscope add <file>` 拷入书库目录。书架屏作源栏 [书架] 入口:标题/作者/进度%/最近阅读时间四列,progress.updated_at desc 排序、无 progress 按 added_at 垫后;Enter 开书(03 票前键位占住即可);进屏扫描 + F5 重扫;屏内无删除操作。i18n 键 zh-CN/en 双语言包(组合成品断言),doctor 加书库行(书数/最近扫描结果/解析警告数)。

**Blocked by:** 01(解析层供元数据与 warnings)。

**Status:** done

## Comments

**票内定拍板(2026-11-19,并工前):**
1. 脏书形态 = **行内 ⚠ 标记**:硬失败(DRM/损坏 zip)的书在书架可见,行首 ⚠ + 标题,Enter 状态栏报类型化错误(DRM→「不支持」、zip→「文件损坏」)。标记来自本次扫描的内存结果,不落库(零 migration 不破);屏内坏书不静默消失。
2. `add` 重名冲突 = **拒绝并提示**:报错给出已存在路径,零写入;更新一本书 = 手动删旧再 add(与「删文件即下架」同 mental model)。

**实现期钉住的语义:**
- **盘上还在但解析变硬失败的书,db 行保留**——只有文件从盘上消失才下架 CASCADE;临时损坏不毁阅读进度/书签。损坏路径进扫描结果 Failed 集,书架行标 ⚠。
- **warnings/扫描结果不落库**:进屏必扫 + F5 重扫,内存即最新;doctor 走同一条 `library.Scan` 缝自扫报活数(书数/损坏/警告),不做第二套代码路径。
- 书架屏沿用壳布局:边栏单入口(全部书籍,计数 badge),列表区四列(标记+标题/作者/进度%/最近阅读);无 progress 的书按 added_at desc 垫后,最近阅读时间列无进度显「—」。

- [x] 扫描:目录递归发现 → 解析 → upsert;同 path 重扫幂等(元数据变化才更新,不变零写)
- [x] 下架:盘上消失的书行清除,progress/bookmarks 级联清(CASCADE 既有)
- [x] 脏书不挡扫:解析硬失败(DRM/损坏)的书不崩扫描,行内 ⚠ 标记(拍板 1),扫描继续
- [x] `readscope add <file>` 拷入书库目录(同名拒绝零写入,拍板 2),拷后即可扫到
- [x] 书架屏四列正确;在读书置顶(最近阅读排序),进度% 来自 progress.pct,无进度书垫后按 added_at
- [x] Enter 开书键位占住(03 前占位提示,03 接管后续读位语义)
- [x] 进书架屏自动扫描,F5 重扫
- [x] i18n zh-CN/en 组合成品断言(书架状态行/scan_done/add 报告/doctor 书库行,zh+en);语言包 parity 测试过
- [x] doctor 报书库行(同 Scan 缝自扫);扫描同步快(千本 fixture 实测 75ms,天花板 10s)

**实现落点(2026-11-19 收口)**:切片 1–5 逐红绿提交(15f25bd 起):store books DAO(三态 UpsertBook:Unchanged/Inserted/Updated)→ internal/library(Scan + Dir ~ 展开)→ ui/bookshelf.go(booksMsg 进屏扫描缝 + bookshelfKeys 拦截 + 四列行组装)→ cmd add → doctor libraryCheck。中途事故:doctor_test.go 被 write 覆盖,M4/M5 既有测试全量恢复后并入新用例(88c3eac 留痕)。

**评审后偏差与挂账(双轴评审 2026-11-19,diff = 675fedc...HEAD,workflow 并行 reviewer)**:
1. **add 拷后解析报告**(标题/警告数/类型化拒绝)——票面只要求拷入;与脏书语义一致,评审判良性超范围,保留待用户追认。
2. 容忍对挂账:bookshelfLine 是第二套列表行组装渲染器(两处容忍线内,第三处必须提炼);app.go hub 承 ~9 个书架触点(Bubble Tea hub 模式先例,观察项)。
3. 评审硬项已修:clampSel 真共享(feed 列表同走)、booksSourceID 常量收敛、"books" 字面量清零;books.err_scan 语义键替换误用的 errors.db_query;doctor 空库注记改展开路径;zh 组合断言补齐两处。全部 go test ./... 绿,gofmt/vet 净,零新依赖。
