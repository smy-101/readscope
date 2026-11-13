# 01: 打开即自动已读 (mark-read-on-open)

**What to build:** Enter 打开任意 feed 条目详情时,静默写入已读。全源统一:RSS / HN / 知乎热榜·关注·日报,含 F3 收藏列表内打开的条目;EPUB 阅读器不涉及。无配置开关,行为写死。

**设计决策(2026-11 grilling 定案)**

1. 触发:打开详情即标,不搞滚到底/停留计时
2. 拉取失败(离线无缓存、只见错误)也标——规则零分支
3. F4 只看未读:接受 Esc 回列表时条目消失、游标落邻条(与手动 Ctrl+R 同构,不新增列表冻结机制)
4. 手动标未读的条目打开仍自动标已读,不做例外记忆
5. 静默落库:不弹"已标已读";手动 Ctrl+R / Ctrl+A 的提示保留
6. 仅对未读条目落库:已读条目重复打开零写入(不刷 read_at、不触发重载)

GLOSSARY「未读点」词条早已写明"打开即消,Ctrl+R 可恢复"——本票让实现回归词条,词条本身不改。

**Blocked by:** 无

**Status:** done (2026-11-13)

- [x] openDetail 对未读条目批一个静默 setRead 写入(复用 stateSavedMsg 重载链)
- [x] 静默:打开后状态栏不弹"已标已读"
- [x] 已读条目打开零写入
- [x] 跨源一致:RSS + HN 测试锁定(知乎/HN 共用同一 openDetail 路径)
- [x] F4:打开后 Esc 回列表,条目从过滤列表消失(测试锁定)
- [x] 重启持久;详情内 Ctrl+R 仍可切回未读且提示正常
- [x] 文档:PLAN 决策表 #15、README 中英键位/特性说明

## Comments

- 实现落点:`openDetail` 对未读条目翻转详情副本 `Read` 并批 `markOpenedReadCmd`(app.go,静默变体,空 status 走同一条 stateSavedMsg 重载链);已读条目零命令。副本翻转是详情内 Ctrl+R 语义正确的前提(否则切出"再标已读")。
- 测试基建:pump/hn 中途态驱动补了 tea.BatchMsg 递归展开(镜像运行时并发展开)——打开详情现在天然是[评论拉取+静默标读]双命令,单命令时代"Batch 压缩回原命令"的偶然依赖不再成立。
- 语义翻转波及三条旧测:详情内 Ctrl+R 从"标已读"变"切回未读"(TestDetailCtrlRCtrlF / TestReadAndStarFromCommentLayers),TestReopenIsCacheFirstThenSilentRefresh 手动展开批取评论腿保序。
- **Review 轮(2026-11-13,两轴均 OK 后修复)**:
  - Standards:①markOpenedReadCmd 与 setReadCmd 形状重复 → 抽出共享 writeReadCmd,两个策略包装(手动带状态、打开即读静默);②改名 markReadOnOpenCmd,消除与 GLOSSARY「开帖标记」avoid 词"opened 位"的擦边;③批解包改按消息类型分发(commentsMsg/stateSavedMsg),不再钉 Batch 成员顺序;④新测试断言改走 i18n 目录(cat.Get),不再硬编码英文串(决策 #8;旧套件的同类硬编码是全局惯例,留待专项清扫);⑤PLAN §7 键位表 Enter 行同步补注。
  - Spec:写失败腿也触发重载(stateSavedMsg 错误分支返回 loadFeedsCmd),库为真、列表收敛,不再与乐观翻转的副本分歧。
