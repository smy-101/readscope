# 09: 文章卡 — zh:article 行 + 两层详情(正文→评论)+ 关注流收编

**What to build:** 文章卡(专栏文章)落地推荐流与关注流——替换两处的丢弃规则为同一个映射:article 卡 → 行 id=`zh:article/<id>`,标题/作者取卡,Ctrl+O/Ctrl+Y 目标 `https://zhuanlan.zhihu.com/p/<id>`,时钟取卡(缺则 FetchedAt)。详情两层(文章天然无回答层):

- **正文层**:Enter 文章卡 → 标题 + 作者 + 正文(`v4/articles/{id}` 的 `content` HTML,经 CleanHTML 进渲染管线;v4 族头矩阵同问题详情,`x-zse-93` 形态 probe 复核)。
- **评论层**:正文内 Enter → `comment_v5/articles/{id}/root_comment`,与回答评论**完全同构**:根评论楼层、子回复扁平化两层带 @parent、缺口补全走 child_comment 游标(单根上限同现值)、Ctrl+N offset 翻页。

`DetailOnDemand` 按 id 前缀 `zh:article/` 分派(与 `zh:question/`、`zh:daily/` 并列的第三种形态);失效语义:文章端点属 v4 族,401/403 → 状态栏 Ctrl+U 引导、不清 cookie、不伤热榜/日报。正文与评论随开帖缓存(comments DAO + 开帖标记复用),离线可读。

**Blocked by:** 08(推荐流先落地,文章卡在 recommend 与 moments 两处映射共用)

**Status:** done (2026-12-14,agent 侧;真网行为已 probe 实测,剩余人工手感走查归 07 号票 2b 节)

- [x] probe:文章正文端点(v4/articles/{id} include 集与头矩阵)+ 文章评论端点(comment_v5/articles)真网判定,样本落盘,契约表复核
- [x] 推荐流 + 关注流 article 卡 → `zh:article` 行(映射共用一个函数;id/URL/作者/时钟;两 feed 的 fixture 都补文章卡断言)
- [x] 两层详情:Enter 文章 → 正文层;正文内 Enter → 评论层;Esc 逐层退;各层滚动状态独立保留
- [x] 评论层:根楼层 + 扁平化 @parent + 缺口补全 + offset Ctrl+N,复用回答评论管线
- [x] 失效语义:v4 族 401/403 → Ctrl+U 引导(不清 cookie);正文空 body 报「站点可能改版」(日报同款自检)
- [x] 缓存:正文/评论开帖缓存离线可读;开帖标记复用;列表刷新不抹缓存
- [x] i18n(zh.article 相关状态键如需)/ F1 / README 收口;httptest 假文章端点集成测试,离线可跑

## Comments

- 2026-11-12 立票。决策:文章详情一步到位(正文+评论,不分期);关注流文章卡顺带收编(同一次映射改动,「关注的人发专栏」本就是关注流应有内容;spec 原「文章卡是后续纯增量」伏笔在此兑现)。pin/video 卡两个 feed 都维持丢弃。
- 文章 id 同款大整数风险:qid 的教训是裸大整数超 2^53 丢精度,article id 一律 json.Number/字符串提取,不碰 float。
- **2026-12-14 实现落地**。真网 probe 全绿(11 GREEN · 0 BLOCKED,样本在 samples/article-*.json)。
- **真网偏差 #1(已回写契约表)**:spec 原写的正文端点 `www.zhihu.com/api/v4/articles/{id}` 已死——403 code 10003,带不带 x-zse-93、有无 cookie 皆拒(probe 双腿实测)。真门是 **`zhuanlan.zhihu.com/api/articles/{id}`**:cookie 必带(匿名 403 code 10003)、无版本头、Referer 专栏页;字段 title/author.name/content(HTML)/comment_count/voteup_count/created/updated 全齐,id 以字符串回传。Hosts 增第四根 Zhuanlan,READSCOPE_ZHIHU_BASE 同步覆盖。评论端点 `comment_v5/articles/{id}/root_comment` 按原契约 GREEN(cookie、免版本头、首页免 offset)。
- 实现形态:详情树 = 单一伪楼层(文章本体,NumChildren=comment_count),评论挂其下;`FetchAnswerComments` 按条目 id 前缀分派 answers/articles 两门,共用 fetchComments 走查(缺口补全/游标契约单点);UI 以 `article` 标志走两层导航(Enter 直入评论层、Esc 评论层直回正文层、正文层页脚计评论总数),零新接口面。

**评审(2026-12-14,Standards+Spec 双轴并行子代理)**:Standards 轴零硬违规,四条 P2 判断题——①enterArticleComments 与 enterReplies 尾部重复→已修(pending 分支外整体委托 enterReplies);②跨包测试夹具重复→维持(需导出 testutil,不值);③article 布尔标志六处分支→维持(每处小而有注,抽象层集合反而 speculative);④aPlane 命名→维持(与问题侧 qPlane/zhihuPlane 对偶命名)。Spec 轴逐项验证通过、无 scope creep,一条 **P1 已修**:正文加载窗口内 Enter → 评论层 pending 屏,正文树落地后无人补发评论请求→「无评论」假屏死路(评论层 Enter 是 no-op);修复=commentsLanded 两腿落地时 maybeKickArticleComments 补发(镜像 commentsPageLanded 的排队进入踢),红→绿测试 TestZhihuArticleEnterDuringLoadKicksComments(门阻塞夹具重构为 articlePlaneWithHold)。另两条 P2:probe_test 死夹具 blockArticleComments→已删;「F1 收口」措辞→F1 全通用键(Enter→open/Esc→back,Ctrl+U 早在),本票无需改动,非缺失。smoke 2b 补慢网双 Enter 回归项。双轴 merge verdict 均 OK。
