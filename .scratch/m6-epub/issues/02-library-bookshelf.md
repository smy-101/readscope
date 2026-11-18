# 02: 书库扫描 + 书架屏

**What to build:** `internal/library` 递归扫描书库目录(`*.epub`):books upsert(path 唯一,元数据+warnings 走 01 解析层),盘上消失的行清除 = 下架(progress/bookmarks 随 CASCADE);`readscope add <file>` 拷入书库目录。书架屏作源栏 [书架] 入口:标题/作者/进度%/最近阅读时间四列,progress.updated_at desc 排序、无 progress 按 added_at 垫后;Enter 开书(03 票前键位占住即可);进屏扫描 + F5 重扫;屏内无删除操作。i18n 键 zh-CN/en 双语言包(组合成品断言),doctor 加书库行(书数/最近扫描结果/解析警告数)。

**Blocked by:** 01(解析层供元数据与 warnings)。

**Status:** pending

- [ ] 扫描:目录递归发现 → 解析 → upsert;同 path 重扫幂等(元数据变化才更新,不变零写)
- [ ] 下架:盘上消失的书行清除,progress/bookmarks 级联清(CASCADE 既有)
- [ ] 脏书不挡扫:解析硬失败(DRM/损坏)的书不崩扫描,形态可编程消费(行内错误标记或排除+doctor 计数,票内定),扫描继续
- [ ] `readscope add <file>` 拷入书库目录(重名冲突处理票内定),拷后即可扫到
- [ ] 书架屏四列正确;在读书置顶(最近阅读排序),进度% 来自 progress.pct,无进度书垫后按 added_at
- [ ] Enter 开书键位占住(03 前占位提示,03 接管后续读位语义)
- [ ] 进书架屏自动扫描,F5 重扫
- [ ] i18n zh-CN/en 组合成品断言;语言包完整性测试过
- [ ] doctor 报书库行;扫描为同步快操作(千本书库规模内)
