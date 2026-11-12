# M4: 知乎源 — Spec

Status: ready-for-agent

上游方案:`docs/PLAN.md` §8 里程碑 M4、ADR #2(经 #14 修订)/#13/#14;术语见 `GLOSSARY.md`(信息流 + Hacker News + 知乎 三节)。

## Problem Statement

用户在终端里想刷知乎:热榜看全站话题、关注流看自己关心的人的新回答、知乎日报读编辑精选全文。当前 readscope 源栏只有 RSS/HN,知乎是占位符。知乎未登录网页有登录墙 + zse-ck JS 挑战(纯 HTTP 客户端不可达),App 无开放 API;PLAN v1 曾把"知乎登录态抓取"整体列为非目标(指浏览器常驻通道,工程量以月计)。2026-11 调研 + FeedScope 源码实测矩阵确认了更轻的路线:**免签 API(只认 cookie)+ 手动贴 cookie 的 Go 直连**,工程量收敛到周级——本里程碑把这条路线落成第四个源。

## Solution

新增 zhihu 源,feeds = 热榜(免登录)/ 关注(cookie)/ 知乎日报(免登录)。列表条目统一为问题卡(回答卡归并到所属问题,文章/想法/视频卡丢弃)与日报条目;详情复用三层详情模型:问题层(描述 + 回答楼层)→ 回答层(单答全文,←→ 切换)→ 评论层(根评论楼层 + 子回复扁平化)。登录仪式 = 手动贴 cookie(独立文件 0600),Ctrl+U 重登;v4 详情端点 401/403 = 会话失效 → 引导重登(不清 cookie),热榜/日报不受影响。全链路缓存先行,离线可读。

## 数据面契约(2026-11 实测 + FeedScope 同源矩阵背书)

| 通道 | 端点 | 鉴权与头 | 翻页 | 备注 |
|---|---|---|---|---|
| 热榜 | `GET api.zhihu.com/topstory/hot-list?limit=10` | 无(仅桌面 UA) | 无(上限 30 条,limit 参数无效) | qid 一律从 `target.url` 提取(裸大整数超 2^53,数值解析丢精度);元信息 = `detail_text`("2045 万热度") |
| 日报列表 | `GET news-at.zhihu.com/api/4/news/latest` | 无 | 无(当日即全量) | hint 作元信息列("作者 · 2 分钟阅读") |
| 日报正文 | `GET news-at.zhihu.com/api/4/story/{id}` | 无 | — | `body` 为 HTML,进渲染管线;存 body 快照离线可读 |
| 关注流 | `GET www.zhihu.com/api/v3/moments?limit=10&desktop=true` | cookie | `paging.next` 游标 | Referer=`https://www.zhihu.com/follow`;只留问题卡(answer 卡归并所属问题,article/pin/video/ad 丢弃) |
| 问题元信息 | `GET www.zhihu.com/api/v4/questions/{qid}?include=detail,excerpt,answer_count,follower_count,visit_count,author,created` | cookie + `x-zse-93`(必带,缺失=403 code 10003) | — | 描述 detail 为 HTML |
| 回答 | `GET www.zhihu.com/api/v4/questions/{qid}/feeds?include=…&limit=5&order=default&platform=desktop` | cookie(**首次入口不带 x-zse-93,带=403**) | `paging.next` | `content` 为 HTML;include 集保持精简形态 |
| 评论 | `GET www.zhihu.com/api/v4/comment_v5/answers/{aid}/root_comment?order_by=score&limit=20&offset=` | cookie(x-zse-93 无害) | offset / `paging.next` | root 只带 1–2 条热门子回复;缺口 = `child_comment_count − len(child_comments)`,补全走 `comment_v5/comment/{id}/child_comment` 游标(单根最多 6 页) |

公共请求形态:桌面 Chrome UA;`x-requested-with: fetch`;Referer 按端点;`x-zse-93: 101_3_3.0`(静态常量,按上表矩阵)。**x-zse-96 签名一律不需要**(2026-11 FeedScope 实测,本源不实现签名器);zse-ck JS 挑战只挂 HTML 页,JSON API 不经过(01 号票复核)。

> **2026-11-12 `zhihu probe` 真网判定:全端点 GREEN,免签直连成立。**x-zse-93 两处门禁均已放宽(questions 不带也 200、feeds 带也 200),按旧矩阵形态请求仍全 200,常量保留;热榜 www 门(`api/v3/feed/topstory/hot-lists/total`)为会话门(匿名 401 code=101),probe 里作备份门;「非浏览器指纹被风控」证伪。详见 01 号票 Comments。

会话语义(FeedScope 用户实测):z_c0 服务端过期后 v3 列表照常、v4 详情一律 403 → v4 详情 401/403 转登录引导;v3 列表 403 视为纯风控,原样报错、不清 cookie(403 无法与风控区分,清了等于强制重登)。

## User Stories

1. 作为读者,我想 Tab 切到知乎就看到热榜,以便不开浏览器了解今天全站在吵什么
2. 作为读者,我想在边栏切 热榜 / 关注 / 知乎日报,以便按需浏览
3. 作为读者,我想热榜行元信息列看到热度("2045 万热度"),以便判断话题热度
4. 作为读者,我想 Enter 打开问题,看到问题描述与回答楼层,以便扫读讨论结构
5. 作为读者,我想楼层头显示 author · 赞同 · N 评论,以便挑选想读的回答
6. 作为读者,我想 Enter 进入回答全文、←→ 在回答间移动,以便连续读多个回答
7. 作为读者,我想回答内 Enter 进入评论区,根评论各成一楼、子回复扁平化带 @parent,以便跟读
8. 作为读者,我想 Ctrl+N 在列表(关注流游标)/ 问题层(更多回答)/ 评论层(offset)各自加载更多,以便按需翻页
9. 作为读者,我想未配置 cookie 时热榜/日报照常可用、关注 feed 显示登录引导,以便零门槛先用起来
10. 作为读者,我想 Ctrl+U 粘贴浏览器复制的 cookie 保存即用,以便不折腾命令行
11. 作为读者,我想 cookie 失效时状态栏提示重登、热榜/日报不受影响,以便失效只是关注流的一层降级
12. 作为读者,我想 Ctrl+O 在浏览器打开当前对象(问题页/回答锚点/日报原文)、Ctrl+Y 复制链接,以便跳出终端无缝
13. 作为读者,我想 ● 未读点 / Ctrl+R / F4 / Ctrl+A / F3 收藏在知乎上与其他源行为一致,以便肌肉记忆通吃
14. 作为读者,我想日报条目 Enter 直接读排版好的全文、离线也可读,以便通勤断网翻旧刊
15. 作为读者,我想 `readscope doctor` 报告知乎 cookie 文件状态,以便排查问题
16. 作为读者,我想新装 readscope 默认就有知乎源(热榜/日报开箱即用),以便零配置可用

## Implementation Decisions

- 新增 `internal/source/zhihu` 包,id `"zhihu"`,init 注册进源注册表;实现 Source + DetailSource + PagedSource(游标变体):`feed_fetch_state` 增 cursor 列(下一号 migration),"下一页 = 当前游标"泛化页深语义,重启从存储游标续翻
- `DefaultSources` 改 `["rss","hn","zhihu"]`;已存在的 sources.toml 不自动改写,README 记手动加法(m3 同款)
- 条目 id:`zh:question/<qid>`(热榜与关注同题同 id,跨 feed 重归属复用既有机制)、`zh:daily/<storyId>`
- 热度复用热度快照 points 列("2045 万热度"折算数值存入),元信息列按源格式化还原;关注流元信息 = 作者名(author 列);日报 = hint
- cookie:`~/.config/readscope/zhihu.cookies`,权限 0600,内容为原样粘贴的 Cookie 请求头文本;解析出 z_c0/d_c0 判活,其余键值原样透传;`readscope zhihu login` 子命令引导粘贴;TUI Ctrl+U 弹输入 dialog;doctor 报告存在/权限/z_c0 有无
- 失效处理:详情族(v4/comment_v5)401/403 → 状态栏"登录态失效,Ctrl+U 重新登录",不清 cookie、不中断热榜/日报;列表族(v3)403 原样报错
- 内容前处理(zhihu 包内的 DOM 清洗,渲染管线不改):剥 noscript 副本;sticker 贴纸取 alt 文本;`a.video-box` 卡片生成 [视频] 链接——href 优先(服务端原给的地址恒可开,防生成式 URL 失效),`data-lens-id` 生成 lens 页仅作 href 缺席时的兜底(03 评审补记:与本文首版「取 data-lens-id 生成」有出入,按 href 优先收口);img URL 属性优先级 data-original > data-actualsrc > src(剥 zhimg `/50/`、`/80/` 缩略代理——`/80/` 为 03 落地时补的变体,有测试;src 为 data: 懒加载占位时跳过);相邻同图标记合并
- JSON 解析统一 `json.Number`,id 类字段一律从 URL/字符串提取
- 评论子回复补全:缺口计算为纯函数;补全请求单根一游标并发上限克制(账号风控优先)
- 缓存:条目快照(items.body)、评论整树(comments 表)沿用既有 DAO;开帖标记复用;日报正文进 body 快照
- 键位:Ctrl+U 登录(全局);F1 帮助收口新键
- i18n:feed 标签 + 登录/失效/降级状态键,zh-CN/en 双语同步,键树一致性测试守住
- 无新第三方依赖

## Testing Decisions

- 只测外部行为;httptest 假知乎服务端 + JSON fixture,覆盖:端点矩阵、头矩阵(x-zse-93 带/不带/错版本 × cookie 有/无;10003 与 401/403 语义分流)、大整数精度、moments 异构卡过滤(answer/question/article/pin/ad 混排,断言只出问题卡且同题去重)
- 评论补全纯函数:缺口计算、热门子回复去重、游标终止条件
- DOM 清洗 golden 样例:noscript/sticker/video-box/缩略代理/重复图
- UI:bubbletea Model 按键泵测试(四层导航、未登录降级、失效引导)
- 真网 smoke(用户 cookie)归 07 号票人工验收,CI 恒离线

## Out of Scope

- 关注流的文章卡/想法卡/视频卡(丢弃;文章卡是后续纯增量)
- 推荐 feed(算法流,与阅读器定位弱相关)
- chromedp 扫码登录仪式(cookie 文件是唯一事实源,通道可后补)
- zse-ck JS 挑战破解、知乎 HTML 页抓取(JSON API 不经过挑战)
- 知乎官方数据开放平台与第三方付费 API
- 盐选/付费内容、想法及其评论

## 风险与止损(01 号票判定)

| 风险 | 对策 |
|---|---|
| 非浏览器 TLS/头序指纹被 JSON API 风控(头号未知) | ✅ 01 已判定(2026-11-12 真网全绿):未被拦,阶梯未动用;后续间歇 403 再上阶梯(头序/UA 微调 → chromedp 一次性登录通道 → 缩回免登录数据面) |
| cookie 数日~数周失效 | 手动贴税已知并接受;Ctrl+U 一键重登;chromedp 增强留缝 |
| x-zse-93 版本号 bump(v4 报 10003) | 常量单点维护;probe 报告现值,替换即愈;01 实测两处门禁已放宽(缺 93 的 questions 与带 93 的 feeds 均 200),常量按旧矩阵保留 |
| 站点改版碎(字段/端点) | `readscope zhihu probe` 落盘原始样本;0 条解析即报"站点可能改版"(FeedScope 同款自检) |
| 账号风控 | 验证期小号;手动 F5 无自动轮询;评论补全并发上限 |

## 里程碑票据

| # | 票 | 估时 |
|---|---|---|
| 01 | 数据面 tracer:`zhihu probe` + cookie 注入 + 止损判定 | 0.5–1 天(+用户配合) |
| 02 | 源包骨架 + 热榜 feed(注册→拉取→入库→列表) | 1–2 天 |
| 03 | 知乎日报 feed(列表 + 全文详情 + 离线) | 1 天 |
| 04 | cookie 管理 + 登录仪式 + 失效语义 | 1–2 天 |
| 05 | 关注 feed(问题卡过滤 + 游标分页) | 1–2 天 |
| 06 | 问题三层详情(回答楼层/回答层/评论层 + 缓存) | 3–5 天 |
| 07 | M4 验收(真网 smoke + README/F1/i18n 收口) | 0.5–1 天 |
