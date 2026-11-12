# 06: 问题三层详情 — 回答楼层 / 回答层 / 评论层 + 缓存

**What to build:** 知乎问题走三层详情(HN 同构):**问题层** = 标题 + 元信息("提问人 · N 个回答 · N 万浏览")+ 描述(HTML 清洗进管线)+ 回答楼层列表(楼层头 author · 赞 N · N 条评论),数据 = v4/questions 元信息(带 x-zse-93)+ feeds 首批 5 条(不带 x-zse-93)两请求;**回答层** = 单个回答全文(←→ 在回答间移动),内容已随楼层拉取;**评论层** = 当前回答的评论,根评论各成一楼、子回复两层扁平化带 @parent——root_comment 每页 20 条(offset 翻页),子回复缺口 = child_comment_count − 已带,child_comment 游标补全(单根≤6 页,并发上限克制)。Ctrl+N 三层各自加载更多(楼层=feeds paging.next;评论=offset;回答层无翻页)。评论整树进 comments 缓存、开帖标记复用:二次打开先渲染缓存,在线后台刷新,离线可读。收藏快照=条目快照(标题/链接/描述),回答/评论走缓存语义不进快照。

**Blocked by:** 04

**Status:** done (2026-12-08)

- [x] 问题层:元信息+描述+回答楼层;两请求编排,首批 5 条;楼层头三元组(author · 赞 N · N 条评论)
- [x] 回答层:Enter 进入单答全文(清洗后管线排版),←→ 楼层间移动,Esc 逐层退,各层滚动状态独立保留
- [x] Ctrl+N:楼层沿 feeds paging.next 追加(去重防排序抖动);到底判定含 answer_count 总数
- [x] 评论层:root 楼层制 + 子回复两层扁平化 @parent;缺口补全纯函数(缺口计算/热门子回复去重/游标终止)单测覆盖
- [x] 评论整树入 comments 缓存 + 开帖标记:二次打开先缓存后刷新,离线可读;楼层/回复层 Ctrl+N 内存分页
- [x] v4 详情 401/403 触发 04 号票登录引导;404 = "问题不存在/已删除"的友好转译
- [x] 内容清洗 golden:noscript/sticker/video-box(data-lens-id)/img 属性优先级/剥 /50/ 缩略代理/相邻同图合并
- [x] httptest 假端点(元信息/feeds 多页/评论多页+子回复缺口 fixture)+ Model 按键泵四层导航测试,全部离线

## Comments

落地 2026-12-08。结构上三层与 HN 完全同构(问题层=body 层、回答层=楼层层、评论层=回复层——"根评论各成一楼、子回复两层扁平化 @parent"恰好就是 `flattenReplies` 的语义,回答即楼层,GLOSSARY 回答层词条背书),UI 侧没有新增层枚举,只有 per-source 分支。

**数据面**(zhihu/detail.go):`Detail` 两请求**并行**(FeedScope 同款:独立门,串行白等一个 RTT;meta 腿错误优先,404 语义不被 feeds 腿淹没);feeds/元信息 URL 与 include 常量复用 probe.go 的实测形态;评论 child_comment 补全:根间并发上限 3、单根 ≤6 页、失败软停(已带热门子回复保留)——上限常量单点。`ErrContentGone`(FamilyDetail 404)进 transport,feeds/评论端点同语义。

**模型/存储**:`core.Content` 增 `Body`/`Question *QuestionInfo`/`Next`(hn 全零值);`core.Comment` 增 `NumChildren`(楼层头"N 条评论"的服务端计数,0→nil 不占位);migration 010 = `comments.num_children` + `detail_fetch.question`(QuestionInfo JSON,meta 三元组离线渲染;SaveComments 的 upsert 不碰该列,整树替换后存活)。`DetailSource` 增逐条目探针 `DetailOnDemand`(BodySource 同款;zhihu 只认问题卡,日报条目不再被误绑详情机制;hn 恒 true)。

**refresh 组合层**:hn 照旧整树替换;zhihu(DetailPager)后台刷新走 `mergeDetailTree` 纯合并——新首页楼层带头,旧楼层**一律保留**(客户端无法区分"上游删除"与"重排下首页",卸载已读评论树是更坏的 miss;被删回答作陈旧尾行留存无害)。两个 Ctrl+N 组合 `DetailFloorsPage`/`DetailCommentsPage` = 拉取 + 读缓存 + merge + 整树回写。

**UI**:问题层 doc = 头部(标题+三元组 meta 行)+ 描述 + 楼层头列表 + footer(loading/failed/N/M 个回答 · Ctrl+N);Ctrl+N 分层:问题层=feeds 游标(会话内存态,重开重走首页;到底 = 游标空或楼层数 ≥ answer_count)、回答层=内存跳 10 层(回答层无翻页)、评论层=窗口还能滚→跳行,全部可见/到底→offset 拉页(全可见时跳行无意义,唯一前进是更多根评论)。计数折法 UI 侧 `zhihuCount`(zh 万一位小数/en K-M,FeedScope display 同规则);Ctrl+O/Y 在楼层/评论层走 `AnswerURL`(裸评论 id 无法独立配对问题页,故挂 DetailPager 而非 CommentURL)。404→`zhihu.question_gone`,401/403→04 号票引导(复用 `zhihuSessionStatus`,detail 路径此前没有接,本次补上 `detailErrStatus`)。

**测试**:zhihu 数据面(fakeZhihu 扩展 404 臂/child 页序列/精确路径断言)、refresh 纯合并矩阵、UI 按键泵(四层导航/滚动独立/Ctrl+N 三层/404/403/离线重开零请求)+ 回答内容组合 golden(answer-body.golden:sticker/video-box href 原样/懒加载双图合并//50/ 剥离)。清洗规则本体 03 号票已全覆盖,未改 clean.go 一行。

**移交 07**:真网 smoke 的三层走查(开 5 问题、热评缺口补全真网行为、Ctrl+U 换号)、README 知乎详情段落、F1 帮助键位收口(评论层 Ctrl+N 语义)、`readscope zhihu probe` 的 -qid 深走(票面本来就在)。
