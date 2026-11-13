# M5: 小黑盒源 — Spec

Status: ready-for-agent

上游输入:`research.md`(2026-11 调研,法务/生态/端点矩阵)、`smoke.md`(2026-11-13 冒烟实测,签名变体裁决 + hex link_id)、Grill 共识(2026-11-13,两轮九项全部定盘);术语见 `GLOSSARY.md` 小黑盒节;PLAN §2 ADR #16。

## Problem Statement

用户在终端 readscope 里已经刷 RSS/HN/知乎,日常还想看小黑盒的社区内容(游戏圈帖子)。小黑盒无官方社区 API,App/Web 之外没有第三方通道;此前不确定纯 Go HTTP 客户端能否够到。冒烟已证实:免登录、纯算法签名(web `hkey`)下,列表/详情/评论三层全部匿名可达。本里程碑把这条已验证的数据面落成第五个源。

## Solution

新增小黑盒源(id `heybox`),零登录、只读。Feeds = 推荐流(免登录个性化)+ 圈流(每圈一 feed,圈列表存 sources.toml,CLI 搜索加圈)。条目统一为帖子卡;详情两层,文章卡同构:帖子层(正文全文 + 页脚「N 条评论」)→ 评论层(根评论成楼 + 楼中楼扁平化带 @parent)。内容降级沿用全源惯例(表情剥前缀、图片 `[图片]` 链接、视频标记+外链);广告卡在列表层丢弃。风控(show_captcha/429/版本门槛)= 当次刷新失败 + 状态栏提示,缓存不受影响。全链路缓存先行,离线可读。

## User Stories

1. As a reader, I want to Tab to the 小黑盒 source in the source rail, so that I can browse the community from the terminal.
2. As a reader, I want a 推荐流 without any login, so that I get algorithm-picked community posts with zero ceremony.
3. As a reader, I want each post card to show title/summary/author/upvotes/comments, so that I can triage without opening.
4. As a reader, I want to subscribe to the circles I care about, so that each 圈 appears as its own feed row in the sidebar.
5. As a reader, I want to add a circle by CLI keyword search, so that I never have to hunt topic_id in web URLs.
6. As a reader, I want to hand-edit the circle list in TOML, so that my config stays backupable and versionable.
7. As a reader, I want to open a post card and read the full body, so that plain text, degraded emoji and image markers all render.
8. As a reader, I want Enter to drop into the comment layer, so that root comments form floors and 楼中楼 flattens with @parent — the same look as HN/知乎.
9. As a reader, I want Ctrl+N on the comment layer to keep loading floors, so that 100+ floor posts are fully readable.
10. As a reader, I want 楼中楼 auto-completion, so that a floor's visible replies match its child_num.
11. As a reader, I want Ctrl+N on 推荐流 and circle feeds to page forward, so that browsing never dead-ends.
12. As a reader, I want cached lists/bodies/comments readable offline, so that commutes work without network.
13. As a reader, I want the existing unread-dot and open-means-read semantics, so that behavior is uniform across sources.
14. As a reader, I want to favourite a post card into a 收藏快照, so that the post survives deletion on the platform.
15. As a reader, I want Ctrl+O to open the original post in a browser (share_url), so that interaction is one keystroke away.
16. As a reader, I want video posts to show title + video marker, so that lists stay fast and details stay honest.
17. As a reader, I want reposts to read like normal posts, so that forwarded content isn't lost.
18. As a reader, I want long-form article posts (HTML) typeset by the render pipeline, so that guides read well.
19. As a reader, I want ad cards silently dropped from lists, so that the list has zero noise.
20. As a reader, I want risk-control refusals to surface as a "try later" status hint, so that I know it's not my fault and my cache is intact.
21. As a reader, I want paging depth to survive restarts, so that re-reads don't refetch known pages.
22. As a bilingual user, I want every new UI string in both zh-CN and en language packs, so that the app stays uniformly bilingual.
23. As a narrow-terminal user, I want the heybox list/detail to reuse the existing layout model, so that narrow windows don't shatter.
24. As a privacy-minded reader, I want anonymous access to carry only an install-time device fingerprint, so that no account identity is involved.
25. As a maintainer, I want the signer pinned by reference vectors in tests, so that an upstream signature change goes red immediately.
26. As a maintainer, I want the web-version parameter behind a switch, so that "version too old" breakage is a one-line fix.
27. As a user, I want doctor to report the heybox source health (circle count, last fetch outcome), so that self-check covers the new source.

## Implementation Decisions

- **源注册**:id `heybox`,package init 注册(与 rss/hn/zhihu 同款);sources.toml enabled 列表启用。
- **Feed 集**:推荐流(固定第一个)+ 圈流(每圈一 feed,TOML 圈表驱动,按声明序);`Feeds()` 只列有圈的圈流(无圈时源仍可用,只有推荐流)。
- **数据面契约**(冒烟实测版,详见 smoke.md):
  - 基址 `https://api.xiaoheihe.cn`;公参精简集 `os_type=web&version=999.0.3&x_app=heybox_website&x_client_type=web&x_os_type=Windows` + `heybox_id=-1`;
  - 签名 = query 参数 `hkey/_time/nonce`(变体 A,只签 canonical path;MD5 + 24 字符表 + GF(2⁸) 混淆校验);
  - 列表:推荐流 `GET /bbs/app/feeds`(`offset/pull/dw`)、圈流 `GET /bbs/app/topic/feeds`(`topic_id/offset/limit`),均 offset 翻页;
  - 详情+根楼层:`GET /bbs/app/link/tree`(**hex link_id**,取自列表条目 share_url,不可本地换算;`result.link.text` 正文,`comments[].comment[]` 楼层,`total_page/has_more_floors` 翻页);
  - 楼中楼:`GET /bbs/app/comment/sub/comments`(`root_comment_id/lastval/limit` 游标);
  - 加圈搜索:`GET /bbs/app/api/search/topic`(`q/offset/limit`,**无签名**)。
- **传输层单点**(zhihu transport 同款纪律):签名与公参、持久 device_id(安装时随机 32hex 落配置目录,全请求复用)、风控哨兵错误族(captcha/限流/版本门槛统一映射「稍后再试」)、页间延时,全部一处持有;预留 999.0.3↔999.0.4 版本开关与 cookie 挂点(登录后门,MVP 不用)。
- **条目**:帖子卡,id `hb:link/<hexid>`;Ctrl+O/Ctrl+Y 目标 = 条目 share_url;热度快照收赞数+评论数;广告卡(content_type 无 linkid)列表层丢弃。
- **详情**:DetailSource 两层——帖子层 = 正文(纯文本块 + 降级标记;文章帖 HTML 走渲染管线)+ 页脚「N 条评论」;评论层 = 根评论成楼 + 楼中楼扁平化两层带 @parent。楼中楼补全:楼层 child_num > 已载子回复数时按 lastval 游标补页,知乎评论补全同构(缺口计算 + 去重 + 游标终止)。
- **翻页**:推荐流/圈流 = PagedSource(offset 页码);评论层 = DetailPager 形状适配(楼层 offset/total_page + 楼中楼 lastval 双游标);页深语义与全源一致(命中前进、空页到底、重启续翻)。
- **降级规则**:`[cube_X]` → `[X]`;`imgs` → `[图片]` 链接标记;视频帖 = 标题 +「视频」标记 + share_url,不解析;转发帖按普通帖渲染(结构存疑,spec 期以真样本校验,异常退化为标题卡)。
- **风控语义**:show_captcha/429/版本过低 = 当次刷新失败 + 状态栏提示,不自动重试、不清缓存、无重登链(匿名源);与网络错误同构呈现,不新增错误 UI 形态。
- **加圈**:CLI `readscope heybox add <关键词>` → 免签搜索 → 编号选择 → 写回 sources.toml;TOML 手改兜底;`heybox probe` 留缝不进 MVP。
- **存储**:全复用既有表(items/comments/body/detail_fetch/feed_fetch_state);圈列表存 sources.toml,无新表、无新 migration。
- **i18n**:新增文案键进 zh-CN/en 双语言包,完整性测试覆盖。
- **doctor**:小黑盒行报圈数与最近拉取结果。

## Testing Decisions

- 只测外部行为,不测实现细节;CI 恒离线,真网 smoke 归用户侧人工验收票。
- **签名器**:对 4 组参考向量恒测(向量由原版 JS 实现[RSSHub/WenfuRainbow]固定输入生成,冒烟期已交叉验证)——防手抄回归、上游换算法第一时间红。
- **传输**:httptest 假小黑盒 + fixtures(冒烟 samples 转正):参数/头矩阵、风控哨兵分流(captcha/429/版本门槛)、device_id 持久复用、页间延时。
- **列表**:广告卡过滤、content_type 混排断言只出帖子卡、offset 翻页、hex id 提取、热度快照落库。
- **详情**:正文降级规则(cube/图片/视频/文章 HTML)、楼层分页(total_page)、楼中楼补全(缺口计算/去重/lastval 终止)——纯函数优先(知乎评论补全同款先例)。
- **UI**:bubbletea 按键泵——两层导航(Esc 逐层退、滚动独立)、风控提示、离线渲染。
- **CLI**:加圈流程(fake 搜索 → 编号选择 → TOML 写回 → 源重建后新圈可见)。
- **i18n**:语言包完整性测试(既有机制)。

## Out of Scope

- **登录态全部**:扫码/贴 cookie 仪式、关注的圈动态、消息通知、收藏面、全站搜索——传输层留 cookie 挂点,UI 零登录。
- **写操作**(评论/点赞/签到/任务)——永久红线(需 AES 写签名,封号风险高)。
- **App 原生签名逆向**(Unidbg/SO 分析)——永久红线(撞使用协议 6.2 反向工程条款)。
- **绕验证码**——永久红线(captcha = 提示,不解)。
- 新闻流、用户动态 feed(数据面已验,等真实需求再收)。
- TUI 内加圈(CLI 先行,TUI 后补)。
- 视频解析/播放、终端图像渲染(PLAN 既有边界)。

## Further Notes

- **条款姿态**(research.md §7):《使用协议》4.3.12 禁自动程序抓取;本项目姿态 = 个人自用、低频(页间延时)、只读、匿名——与知乎 M4 已接受风险同量级;三条红线进 PLAN 非目标。
- **失效预期**:RSSHub 时间线约一年一坏、可修量级;version 开关是第一道止损,免签搜索天然是数据面探针。
- **里程碑**:PLAN M5(EPUB 顺延 M6、发布顺延 M7);粗排:①源包骨架+传输+推荐流 → ②圈流+加圈 CLI → ③详情+评论层 → ④UI/i18n/doctor+真网 smoke;估 1–1.5 周;票据拆分归 `/to-tickets`。
- **引用**:调研 `research.md`、冒烟矩阵 `smoke.md`(复现脚本 `smoke/`、`smoke/probe/`)、GLOSSARY 小黑盒节(圈/帖子卡/加圈/推荐流)。
