# M3: Hacker News 源 — Spec

Status: ready-for-agent

上游方案:`docs/PLAN.md` §8 里程碑 M3、ADR #2/#11/#12/#13;术语见 `GLOSSARY.md`(信息流 + Hacker News 两节)。

## Problem Statement

用户在 TUI 里 Tab 切到 Hacker News,边栏与列表全空、状态栏无任何解释——HN 只是源栏里的占位符,数据源从未实现;书架(books)同样空白。想无账号读 HN 的帖子与评论,当前必须离开终端开浏览器。另外,切到未实现源时的谜之空白本身就是一个 UX 缺陷。

## Solution

以 hn.algolia.com 公共索引为数据面实现 HN 源,填进 M2 已就绪的源无关机制里:边栏固定 Front / New / Ask HN / Show HN 四类 feed;列表行元信息列显示 "N pts · N comments · author" 热度雷达;帖子详情走三层结构(正文 → 楼层 → 回复树),深层回复压成两层、跨层带 @parent 前缀;评论缓存先行渲染、在线后台刷新,离线可读。未实现源(书架)切换时状态栏显示"后续里程碑提供"。

## User Stories

1. 作为读者,我想 Tab 切到 HN 就看到 Front 页帖子列表,以便不开浏览器了解今天热帖
2. 作为读者,我想在边栏切换 Front / New / Ask HN / Show HN,以便按热度与话题浏览
3. 作为读者,我想在列表行看到 "N pts · N comments · author",以便判断哪些帖子值得进
4. 作为读者,我想 Enter 打开链接帖直接看到一行原文链接和评论区,以便快速进入讨论
5. 作为读者,我想打开 Ask/Show 文本帖看到排版好的全文,以便在终端读完
6. 作为读者,我想顶层评论各成一楼、楼层头显示 author · score · N replies,以便扫读讨论结构
7. 作为读者,我想 Enter 进入一楼的回复树,以便跟读子讨论
8. 作为读者,我想深层回复压成两层、跨层带 @parent 前缀,以便窄终端里仍看清谁回复谁
9. 作为读者,我想无回复的楼层不响应 Enter,以免进到空层
10. 作为读者,我想 Ctrl+N 在列表/楼层/回复层各自独立加载更多,以便按需翻页
11. 作为读者,我想离线重启后 HN 列表和开过的评论仍可读,以便断网也能翻
12. 作为读者,我想再次打开帖子先立即看到缓存评论、在线时后台静默刷新,以便开帖不卡
13. 作为读者,我想 ● 未读点 / Ctrl+R / F4 / Ctrl+A / F3 收藏在 HN 上与 RSS 行为一致,以便肌肉记忆通吃
14. 作为读者,我想 Ctrl+O 打开当前对象(列表行=原文链接,回复层=该评论 HN 页)/ Ctrl+Y 复制对应链接,以便跳出终端时无缝
15. 作为读者,我想新装 readscope 默认就有 HN 源,以便零配置可用
16. 作为读者,我想 Tab 切到未实现的源(书架)时状态栏提示"后续里程碑提供",以免面对空白不知所措

## Implementation Decisions

- 新增 hn 源包,init 时以 id `"hn"` 注册进源注册表,实现 Source 接口(ID/Feeds/Fetch);与 rss 包同款模式,"加源=加一个包"保持成立
- 四类 feed 映射 Algolia 端点:Front = `tags=front_page`,New = `search_by_date`,Ask = `tags=ask_hn`,Show = `tags=show_hn`;feed 标签走 i18n
- 条目 id 形如 `hn:algolia:<storyID>`(初始 schema 注释既定);链接帖 body 存一行文章链接,文本帖存 story_text(HTML,进 M1 渲染管线)
- 源缝新增可选扩展 DetailSource(`Detail(ctx, item)`),hn 实现:调 `/items/<id>` 一次取整棵评论树;与 ConditionalSource 同款"可选扩展"模式
- comments 表(初始 schema 已建)补 DAO:按条目整树写入/读出;HN 不实现条件请求,刷新恒为无条件
- items 表通过 migration 增加 HN 热度快照字段(points、评论数),值取 fetched 时刻,刷新时更新
- 元信息列按源格式化:HN 行 "N pts · N comments · author"(作者可缩略);RSS 行保持现状
- F5 = 重拉当前 feed 第一页合并进缓存;Ctrl+N(列表)= 拉下一页追加;楼层/回复层的 Ctrl+N 为内存分页(开帖已拉全树,零额外请求)
- 详情层从单层扩成三层状态机:正文层 → 楼层层 → 回复层;←→ 在楼层间移动;Esc 逐层退;首次打开无缓存时显示加载态
- 两层扁平化 + @parent 前缀是纯函数:评论森林 → 两层行序列
- DefaultSources 改为 `["rss","hn"]`;已存在的 sources.toml 不自动改写,README 记录手动加法
- Tab 切到未注册源时状态栏 = status.todo("后续里程碑提供")
- i18n:HN feed 标签与新增状态键,zh-CN/en 双语同步,键树一致性测试守住
- 收藏快照存帖子本身(标题/链接/story_text),不含评论树

## Testing Decisions

- 只测外部行为,不测实现细节
- 源缝:httptest 假 Algolia 服务端 + JSON fixture,断言条目映射与评论树解析;先例:rss 源测试的 serve 助手
- store 缝:comments DAO 与热度字段对真 SQLite;先例:items/readstate 测试
- UI 缝:bubbletea Model 级按键泵测试(楼层导航、元信息列、加载态、空白源提示);先例:list/detail 测试的 seededModel
- 扁平化纯函数:构造深树,断言两层结构与 @parent 前缀
- 评论 HTML 的排版复用 M1 golden 管线,不在 M3 重测管线本身

## Out of Scope

- 抓取外部原文(付费墙/反爬/排版各异,FeedScope 也不抓;Ctrl+O 已覆盖)
- Best / Jobs / Poll 专列(poll 帖混进列表时按文本帖渲染即可);HN 关键词订阅(hnrss 是 RSS 的活)
- HN 站内搜索、点赞/回复等账号功能
- F2 源/订阅管理对话框(维持 status.todo)
- 书架源本体(M4)——只修空白提示

## Further Notes

- Algolia 索引滞后数分钟;Front 列表按 points 排序而非 HN 官方排名——FeedScope 同源限制,文档与帮助里不承诺官方排序
- 全部键位沿 PLAN §7 全非打印键模型;新交互(楼层 Enter/←→/Ctrl+N)进 F1 帮助
