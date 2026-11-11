# M3 真机验收记录

对应 ticket 06。PLAN 验收标准:**无账号完整读帖跟评**。自动化部分已在本会话完成并留证;交互手感部分由用户在真机上复核。

## 自动化验证(2026-11-11,本会话)

| 项 | 证据 |
|----|------|
| CI 等价三步 | `go vet ./...`、`go test -race ./...`、`go build ./...` 全绿(与 ci.yml 步骤一致);远端 CI 待 push 后绿 |
| 四类 feed 刷读 | 源测试 `TestFeedsAreTheFixedFour` / `TestFetchHitsEachFeedEndpoint`(httptest 假 Algolia);UI `TestHNTabShowsFeedsAndCachedItems` / `TestSidebarFeedSwitchLoadsItems` |
| 开帖读文、三层通 | `TestThreeLayerNavigationKeepsState`(层间状态保留)、`TestCommentHTMLThroughPipeline`(评论 HTML 过 M1 管线)、`TestBuildFloors*` / `TestFlattenReplies*`(楼层/两层扁平化纯函数) |
| 离线重启列表即现、评论可翻 | `TestStartupRendersFromCacheOffline` + `TestOfflineReopenReadsAllThreeLayers`(零命令泵=零网络);store `TestFetchStateSurvivesReopen` / `TestCommentsRoundTripPreservesTree` |
| 读态键位对 HN 生效 | ● 未读点:`TestHNTabShowsFeedsAndCachedItems`;Ctrl+R/F4/Ctrl+A:`TestHNReadStateKeysWorkOnHNRows`(本票新增,HN 行上直钉);Ctrl+F+F3:`TestFavoritesRenderMetaPerSource` + `TestHNFavoriteSurvivesFeedEviction`(本票新增);楼层内 Ctrl+R/Ctrl+F:`TestReadAndStarFromCommentLayers` |
| 收藏消失仍可读 | store `TestSetStarredSnapshotsAndSurvives`(快照回填);UI `TestHNFavoriteSurvivesFeedEviction`(HN 行蒸发后 F3 可列可读) |
| F1 帮助覆盖 M3 键位 | `TestHelpAndHintsCoverTheLayers`:F1 钉 Ctrl+N **与 ← → 楼层导航**(← → 断言本票补上);键位数据 `helpBindings` 含 `floor_nav`/`load_more` |
| zh-CN / en 键树一致 | `TestParityAllCatalogsShareKeyTree` 绿 |
| README 双语对齐 | 双语 HN 章节补齐三层阅读结构(楼层+两层扁平化+@parent,对齐 ADR #13 与 GLOSSARY 术语);里程碑状态行改为 "M2+M3 已可用";词汇对齐 GLOSSARY(楼层/回复树/扁平化/热度快照) |

## 真二进制冒烟(本会话,真网)

主会话可真连 hn.algolia.com,故真机冒烟直接执行:构建产物 `go build ./cmd/readscope`,pty 驱动按键(脚本 `/tmp/readscope-smoke/smoke.py`,一次性、不入库),环境隔离(`READSCOPE_CONFIG`/`READSCOPE_STATE` 指向临时目录)。

| 轮 | 按键序列 | 结果 |
|----|---------|------|
| 1 拉取 | Tab Tab F5 | Front 页 20 条真实热帖入库(items 20 行,points/num_comments 快照齐);截屏含 "867 pts · 311 comments · skittl…" 热度雷达、● 未读点、边栏四 feed 计数 "Front 20" |
| 2 重启 | (无 F5,仅切换) | 列表即现:同一 DB 重启,未按任何刷新键,20 行缓存秒出,零 "refreshing" 态 |
| 3 开帖 | Enter(→楼层)… Esc×3 | 链接帖正文一行原文链接("https://…");Detail 真网拉全树:**310 条评论入 comments 表**、detail_fetch 打标;楼层计数 "64 floors" 上屏 |

退出码均 0。

## 真机清单(用户复核,可选)

手感与长时使用面(自动化够不着):

- [ ] 四 feed 各刷一轮,楼层层 ←→ 移动、回复层 Ctrl+N 翻页的手感
- [ ] 窄终端(如 70 列)下热度雷达缩略、标题不折行
- [ ] 真断网状态下重启(冒烟轮 2 用"零刷新键"近似,真断网更彻底)

执行日期与备注:

> (在此记录)
