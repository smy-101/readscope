# 调研:readscope 新增「小黑盒」源 —— 社区功能查看可行性

> 调研日期:2026-11(所有 GitHub 仓库状态、issue、文档均在本轮实际抓取核对)。
> 方法:仅用 web 检索 + 抓取源码原文(未运行任何代码,未发起带签名的真实请求;沙箱内仅实测过一个免签名 GET,见 §4.4)。

## 太长不看(TL;DR)

> **2026-11-13 更新**:后续冒烟实测已裁决 §6.1 签名变体矛盾(A↔999.0.3 / B↔999.0.4 按版本分派)并发现 hex link_id 关钥,免登录三层全部跑通——见同目录 `smoke.md`。

1. **能做,推荐做「只读 + 可选手动贴 cookie」路线,与知乎 M4 同量级甚至略轻**。社区帖子的列表/详情/评论三层端点在多个 2026 年仍活跃的开源项目中被完整逆向(Go 的 `heybox-cli`、TS 的 VSCode 插件 `WenfuRainbow/heybox`、Python 的 `astrbot_plugin_xiaoheihe_adapter`、RSSHub 生产路由),端点、参数、分页、JSON 结构全部有源码级佐证。
2. **签名是纯算法,不需要浏览器/JS 运行时/Unidbg**:web 端读接口只需为每个请求生成查询参数 `hkey + _time + nonce`(MD5 + 字符表变换,Go 参考实现 `heybox-cli/internal/api/signer.go` 约 110 行可直接移植);App 原生签名才需要 Unidbg 模拟 SO,readscope 不碰它。
3. **免登录可达面**:新闻流 `/bbs/app/feeds/news`、帖子正文 `/bbs/app/api/share/data/`、用户资料/动态 `/bbs/app/profile/*` 在 RSSHub 生产环境以 `heybox_id=-1` 匿名调用;话题/板块流、帖子树多数项目匿名可用,但部分项目(全站搜索、消息、收藏)明确需要登录 cookie(`pkey`)。带登录态后可解锁:关注的圈动态、消息通知、收藏。
4. **风控中等**:有验证码(`show_captcha`)、429 限流、版本参数门槛("您的版本过低")的历史,RSSHub 路由约每年坏一次再被修;**写接口**(评论/点赞)还要额外的 `_rnd` AES-256-GCM 写签名,风险明显更高,建议 readscope 只做读。生态热度是重要旁证:GitHub 全站 91 个 xiaoheihe 相关仓库,2026-11 上旬仍有多个在每日 push。
5. **法务与知乎路线同量级但要写清楚**:小黑盒《使用协议》4.3.12 明文禁止"机器人软件、蜘蛛软件、爬虫软件等任何自动程序……获取平台的服务、内容、数据"(条款原文见 §7)。个人、低频、只读、自用 cookie 的量级风险与知乎 M4 相当;批量采集/写操作/绕验证码则会显著升级风险。

---

## 1. 官方开放性

**结论:小黑盒有「开放平台」,但它开放的是小程序工坊和黑盒语音机器人,不开放社区内容 API。社区数据没有任何官方第三方通道,只能走 web/App 客户端逆向接口。**

- 官方开发者文档 `docs.xiaoheihe.cn` 只有 `@heybox/hb-sdk`(小程序工坊):小程序运行在小黑盒 App 受控沙箱内,"只能使用小程序工坊公开的 SDK 能力",`network` 权限"只能访问公开支持的服务,不能使用网络能力绕过平台授权"。来源:[什么是小程序工坊 | @heybox/hb-sdk](https://docs.xiaoheihe.cn/hb_sdk/guide/)(直接证据)。
- 开放平台入口 `open.xiaoheihe.cn` 面向小程序列表;`bot.xiaoheihe.cn` 是黑盒语音(chat.xiaoheihe.cn)机器人平台,其《开发者服务协议》约束的是"利用小黑盒提供的 API 和(或) SDK 向黑盒语音用户提供作品",与社区帖子无关。来源:[黑盒语音机器人开发平台协议](https://bot.xiaoheihe.cn/protocol/bot)、[开发者教程(Apifox 镜像)](https://s.apifox.cn/43256fe4-9a8c-4f22-949a-74a3f8b431f5/5031038m0)(直接证据)。
- 第三方共识表述:`heybox-cli` README:"**小黑盒没有公开、稳定的社区搜索 API**。本项目使用当前网页端/客户端的只读接口"。来源:[shadowfish07/heybox-cli](https://github.com/shadowfish07/heybox-cli)(直接证据);AstrBot 适配器同样声明"小黑盒相关接口属于非公开、可能变化的客户端契约,不是平台承诺稳定的自动化 API"。来源:[astrbot 契约文档](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md)(直接证据)。
- **官方 Web 端形态**:社区 SPA 挂在 `www.xiaoheihe.cn/app/bbs`、`/app/topic/link`、`/app/user/profile`、`/app/user/favour`、`/app/search` 等路径下(来源:[k1m0206/better-XiaoHeiHe](https://github.com/k1m0206/better-XiaoHeiHe) README「支持页面」,直接证据);SPA 的 JS 模块托管在 `static.max-c.com/static/heybox/webapp/@heybox-spa/website/app/`,页面数据全部走 `api.xiaoheihe.cn` 的 JSON 接口(来源:[AaronZ345/xiaoheihe-cli 接口契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md),2026-10-10 核对,直接证据)。任务背景里提到的 `game.xiaoheihe.cn` 未在抓取的任何项目里作为数据面出现,主战场是 `api.xiaoheihe.cn` + `www.xiaoheihe.cn` SPA(研究者注:`game.xiaoheihe.cn` 的可达性本轮未验证)。
- **Web 接口是否需要签名:按端点区分**。部分端点(topic/game 搜索、用户资料/动态)在开源实现里完全不带签名;核心社区端点(feeds、link/tree、搜索 v1)带 `hkey/_time/nonce` 查询参数签名(见 §3)。没有任何证据表明存在 `x-b3` 之类的**签名头**——所有材料里签名都是 **query 参数**形式(研究者核对全部抓取源码,未见 header 签名)。

---

## 2. 已知逆向项目矩阵(核心)

### 2.1 项目清单

| 仓库 | 语言 | 维护状态(本轮核对) | 覆盖面 | 备注 |
|---|---|---|---|---|
| [shadowfish07/heybox-cli](https://github.com/shadowfish07/heybox-cli) | **Go** | 2026-07 创建,持续发布(Homebrew/GoReleaser),活跃 | 只读:全站搜索(帖子/话题/用户/游戏)、帖子正文、根评论分页、楼中楼、**终端扫码登录** | **readscope 最直接参考**:纯 Go、`internal/api/signer.go` 是现成 Go 签名实现,MIT |
| [WenfuRainbow/heybox](https://github.com/WenfuRainbow/heybox) | TypeScript(VSCode 插件,亦上架 Marketplace「HeyBox Forum」) | 2026-07 创建,2026-09-19 最后 push,活跃 | 推荐 `/bbs/app/feeds`、板块目录、话题流、帖子树+楼中楼、搜索、消息、收藏、官方消息、折扣消息、扫码登录 | `src/api/client.ts` + `signature.ts` + `src/types/index.ts` 是最完整的 web 数据模型定义 |
| [674537331/astrbot_plugin_xiaoheihe_adapter](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter) | Python | v1.3.6,2026-11-12 仍有 push,活跃 | 端点契约文档最严谨:QR 登录、权限检查、消息轮询、帖子树、**真实分区目录/分区流**、评论写入(含 `_rnd` 写签名) | `docs/xiaoheihe-api-contract.md` 是端点+参数+验证状态的一手表 |
| [foxlesbiao/xiaoheihe-mcp](https://github.com/foxlesbiao/xiaoheihe-mcp) | Python(只读 MCP Server) | 2026-10 仍在更新(配套博客 2026-10-23) | 30+ 工具:搜索/话题/游戏/帖子/评论/用户帖子/首页推荐/热搜/新闻/游戏库/关注列表/折扣/登录态导入 | 仓库不含签名源码,签名器外部命令;优先 Unidbg 原生签名,可回退公开 Web 签名;另有活跃 fork [huajiao1998/xiaoheihe-mcp](https://github.com/huajiao1998/xiaoheihe-mcp)(2026-11-12 push,"6 处补丁 + 4 个关注工具") |
| [AaronZ345/xiaoheihe-cli](https://github.com/AaronZ345/xiaoheihe-cli) | 未详(有 CLI) | 契约文档 2026-10-10 核对(App 1.3.396) | web 读+写契约:登录恢复、扫码、短评读写、评论、楼中楼游标、搜索、创作者列表、媒体上传 | `docs/api-contract.md` 补齐了分页/游标契约细节 |
| [DIYgod/RSSHub xiaoheihe 路由](https://github.com/DIYgod/RSSHub/tree/master/lib/routes/xiaoheihe) | TypeScript | 生产运行多年,2024-03 大修后存活 | `/news`(新闻)、`/user/:id`(用户动态)、`/discount`、`/add2cart` | `util.ts` 内置完整 web `hkey` 算法("Copied from huandu/heybox-url") |
| [HadeonYu/heybox-bot](https://github.com/HadeonYu/heybox-bot) | **Go** | 2026-05 创建 | 评论机器人(白名单/限流/多模型) | Go 侧第二参考;AstrBot 致谢其"Web 登录参数和客户端身份形状" |
| [XiaHouSheng/heybox-core](https://github.com/XiaHouSheng/heybox-core) | Python + JS 签名器 | 活跃(README 完整) | 机器人框架:轮询 `/bbs/app/user/message`、帖子树、评论发送、QR 登录 | 签名靠 Bun 跑 `encoder_cli/process_origin.js`;建议轮询 ≥10s"避免封号" |
| [huandu/heybox-url](https://github.com/huandu/heybox-url) | JS | **ARCHIVED**,作者声明"不再适用最新 API" | 最早的 web url-hash 算法包 | RSSHub 的算法即源于它;存档但算法仍被 RSSHub/heybox-cli 沿用(见 §5 矛盾记录) |
| [chr233/pyxiaoheihe](https://github.com/chr233/pyxiaoheihe) / [SCUTJcfeng/xiaoheihe](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py) / [zyi1992/xhh_auto](https://github.com/zyi1992/xhh_auto) | Python | 2020 年代,**已停更** | App 侧老接口(签到/任务/关注列表),`heybox_id+imei+pkey` cookie 模式 | 明确警告"使用第三方客户端有封号风险";App `hkey` 是 JNI 计算,"暂未适配" |
| [ypq123456789/BlackboxSign](https://github.com/ypq123456789/BlackboxSign) | JS(包裹官方混淆脚本) | 活跃 | 签到/任务 | cookie 格式 `heybox_id#pkey=…;x_xhh_tokenid=…`;零侵入设计以"避免触发防篡改机制" |
| [Cansiny0320/heybox-daily](https://github.com/Cansiny0320/heybox-daily) | TypeScript/Docker | 66 stars,2026-11-12 push,活跃 | 自动签到 + 每日任务,支持在线托管 | 生态热度与 cookie 配置形态的旁证 |
| [Mrmiaomrzh/BetterHeybox](https://github.com/Mrmiaomrzh/BetterHeybox) (+ [HeyboxAPK](https://github.com/Mrmiaomrzh/HeyboxAPK)) | Java(LSPosed) | 2026-08 起,活跃 | App 内 hook 增强;APK 自动归档仓库用于逆向 | 免责声明:"可能存在账号封禁、数据异常等风险" |
| [GUAPIXIA/xiaoheihehe](https://github.com/GUAPIXIA/xiaoheihehe) | JavaScript | 22 stars,2026-11-11 push | 第三方小黑盒 Windows 客户端 | 证明第三方全功能客户端路线可行 |
| 任务线索 `nonebot-plugin-hbrother` | — | **确认不存在于公开渠道** | — | GitHub 全站搜 `hbrother` 仅 6 仓库且全部无关;PyPI 404;NoneBot Registry 无此插件(见 §8) |

其余相关:DSH 插件系([plasta2steel/dsh-xiaoheihe](https://github.com/plasta2steel/dsh-xiaoheihe)、[fbbDirge/dsh-xiaoheihe](https://github.com/fbbDirge/dsh-xiaoheihe)(自动刷帖,2026-11-13 push)、[lumingfei004/dph-heybox](https://github.com/lumingfei004/dph-heybox)(扫码登录+点赞+生物钟));机器人运营侧([HannJia/xiaoheihe-operator](https://github.com/HannJia/xiaoheihe-operator)、[Yamabuki-Kirio/astrbot_plugin_p5x_feed](https://github.com/Yamabuki-Kirio/astrbot_plugin_p5x_feed)(小黑盒精选/热帖推送));网页增强([admin05/xiaoheihe](https://github.com/admin05/xiaoheihe) 用户脚本、[k1m0206/better-XiaoHeiHe](https://github.com/k1m0206/better-XiaoHeiHe) 浏览器扩展);黑盒语音官方 Go demo [QingFengOpen/HeychatDemo](https://github.com/QingFengOpen/HeychatDemo)(语音平台,非社区);C# 的 [gehongyan/HeyBox.Net](https://github.com/gehongyan/HeyBox.Net)(黑盒语音 SDK);2020 逆向博客 [Chr_ 小屋](https://blog.chrxw.com/archives/2020/08/05/1347.html)系列(App `hkey` JNI 史、免 root 抓凭据教程)。

**生态快照(2026-11-13 检索时点)**:GitHub 按更新排序搜 `xiaoheihe` 共 91 个仓库,头部 10 余个在 2026-10/11 均有 push(签到、MCP、机器人适配器、第三方客户端、DSH 插件全面开花)。来源:[GitHub search API `q=xiaoheihe&sort=updated`](https://api.github.com/search/repositories?q=xiaoheihe&sort=updated)(直接证据)。这说明:① 逆向接口当前真实可用;② 平台未对该生态做一刀切封杀(与 RSSHub 存活多年的旁证一致)。

### 2.2 端点矩阵(社区查看三层)

基址:`https://api.xiaoheihe.cn`。「web 公参」= `os_type=web&app=heybox&client_type=web&version=999.0.3|999.0.4&x_client_type=web&x_app=heybox_website&x_os_type=Windows|Mac&device_info=Chrome&device_id=<32位hex>&heybox_id=<uid或-1>`(来源:[WenfuRainbow client.ts getCommonParams](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts)、[heybox-cli client.go getEnvelope](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go),直接证据)。

#### 列表层(帖子流)

| 端点 | 方法 | 必需参数 | 登录 | 分页 | 出处 |
|---|---|---|---|---|---|
| `/bbs/app/feeds` | GET | web 公参 + `offset`、`pull=0`、`dw=800`;签名 | 匿名可用(MCP:"公开浏览默认无需登录")/AstrBot 标需登录(见 §5 矛盾) | `offset` 递增,返回 `links[]` | [WenfuRainbow client.ts getFeed](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts)、[astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md) |
| `/bbs/app/feeds/news` | GET | `os_type=web&…&heybox_id=-1&appid=900018355&offset=0&limit=20` + `hkey/_time/nonce`(RSSHub 算法) | **免登录**(`heybox_id=-1`) | `offset/limit` | [RSSHub news.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/news.ts)(直接证据) |
| `/bbs/app/topic/feeds` | GET | `topic_id`、`offset`、`limit(1..30)`、`lastval`、`dw=720` | AstrBot 标需登录;heybox-cli/MCP 路线匿名可用 | `offset` + `lastval` 游标,返回 `links[]` | [astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md)、[WenfuRainbow getTopicFeeds](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts) |
| `/bbs/app/topic/categories` | GET | web 公参;签名 | WenfuRainbow 走登录态 | — | [WenfuRainbow client.ts](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts) |
| `/bbs/app/api/topic/index/?type=list` | GET | web 公参;签名 | 是(AstrBot) | —(目录一次性,返回 `topics_list` 递归树) | [astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md) |
| `/bbs/app/profile/events` | GET | `userid`、`list_type=moment`、`version` | **免登录、无签名**(RSSHub 生产) | —(moments 全量返回) | [RSSHub user.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/user.ts) |
| `/bbs/app/link/followed/post`(关注的人的动态)、`/bbs/app/api/follow/alert` | GET | App 侧参数 `heybox_id/imei/os_type/version` + cookie `pkey` | 是(pkey) | 老端点,2020 年代码即有 | [SCUTJcfeng client.py](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py)(直接证据,现况未复核) |
| `/bbs/app/profile/following/list` / `follower/list` | GET | 同上 App 侧 | 是(pkey) | offset | [SCUTJcfeng client.py](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py) |
| `/bbs/app/api/search/topic`、`/game/search/` | GET | `q/offset/limit` | **无签名**(heybox-cli 实现里 `signed=false`) | offset/limit | [heybox-cli client.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)(直接证据) |
| `/bbs/app/api/general/search/v1` | GET | `q`、`search_type=link|user`(heybox-cli 同时发 `keyword`、`tab_type` 兼容)、`offset/limit`、`sort_filter`;签名 | **是**(`lack_token`/`need_login`),且偶发验证码 | offset;网页版每页 30 | [heybox-cli client.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)、[AaronZ345 契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md)、[xiaoheihe-mcp README](https://github.com/foxlesbiao/xiaoheihe-mcp) |

#### 详情层

| 端点 | 方法 | 必需参数 | 登录 | 分页 | 出处 |
|---|---|---|---|---|---|
| `/bbs/app/link/tree` | GET | `link_id`;web 侧 `offset/limit/sort_filter`(heybox-cli/WenfuRainbow);web 契约另记 `page/is_first/index/owner_only/limit` + `offset/sort_filter` 兼容;签名 | 匿名多数可用(MCP);AstrBot 标需登录 | 根评论楼层:`has_more_floors` 标志翻页 | [heybox-cli](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)、[WenfuRainbow getPostTree](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts)、[AaronZ345 契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md) |
| `/bbs/app/api/share/data/` | GET | `link_id`、`heybox_id=-1`、`use_concept_type=`、web 公参 + `hkey/_time/nonce` | **免登录** | — | [RSSHub news.ts/user.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/user.ts)(直接证据) |
| `/bbs/app/api/original/image` | GET | `url`(把展示图换原图) | 登录(WenfuRainbow) | — | [WenfuRainbow client.ts](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts) |
| 分享短链 `https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=…` | GET | `link_id` | 免登录(RSSHub 用作 item link) | — | [RSSHub](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/news.ts)(这是任务提示中 `/v3/bbs/...` 形态的实际出处) |

#### 评论层

| 端点 | 方法 | 必需参数 | 登录 | 分页 | 出处 |
|---|---|---|---|---|---|
| `/bbs/app/link/tree` | GET | 同详情层;根评论按楼层分页 | 同上 | `has_more_floors`;契约记 `page/is_first/index/owner_only/limit` | [AaronZ345 契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md) |
| `/bbs/app/comment/sub/comments` | GET | `root_comment_id`、`lastval`(=上一页最后一条评论 ID)、`limit`;heybox-cli 另带 `link_id` | 匿名多数可用 | `lastval` 游标 + `has_more` | [AaronZ345 契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md)、[heybox-cli GetCommentReplies](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)、[WenfuRainbow getSubComments](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts) |

#### 登录/账号层(readscope 可能用到)

| 端点 | 方法 | 参数 | 登录 | 出处 |
|---|---|---|---|---|
| `/account/get_qrcode_url/?app=web` | GET | `app=web`(匿名会话) | 否 | [heybox-cli CreateQRLogin](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)、[astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md) |
| `/account/qr_state/?app=web&qr=<token>` | GET | 轮询;返回 `heybox_id`、`pkey`、`x_xhh_heyboxid`、`expire_at`;状态 `wait/ready/ok/过期`,账号被风控时 `need_google_check` | 否 | 同上(直接证据) |
| `/account/restore_login` | GET | 用已有 cookie 恢复身份 | 是 | [AaronZ345 契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md) |
| `/bbs/app/user/message` | GET | `list_type=0`(评论)/`message_type=4|16|17`(关注/@)、`offset/limit/no_more` | 是 | [astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md)、[WenfuRainbow getMessages](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts) |
| `/bbs/app/profile/fav/folder/v2/links` | GET | `enable_new_style_collect=1`、`offset/limit/dw` | 是 | `has_next` | [WenfuRainbow getFavouriteLinks](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts) |

写操作端点(评论 `POST /bbs/app/comment/create`、点赞 `POST /bbs/app/profile/award/link`、收藏 `POST /bbs/app/link/favour` 等)有完整逆向,但需要 `_rnd` 写签名(AES-256-GCM,2026-11 从 v15 升到 v16),readscope 明确不做,不展开。来源:[astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md)、[SCUTJcfeng client.py](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py)。

### 2.3 鉴权与签名体系(字段语义)

| 字段 | 是什么 | 哪些端点需要 | 出处 |
|---|---|---|---|
| `heybox_id` | 用户数字 UID,也是 cookie 名之一 | web 公参(匿名传 `-1`);App 侧必带 | RSSHub(`heybox_id=-1`)、[SCUTJcfeng client.py](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py) |
| `pkey` | 登录态 cookie 值(长期凭证),App 抓包或 Web 扫码获得 | 所有"登录后"端点;cookie 头 `Cookie: pkey=…`(常与 `heybox_id`、`x_xhh_tokenid` 并存) | [Chr_ 凭据抓取教程](https://blog.chrxw.com/archives/2020/08/10/1353.html)、[heybox-cli PollQRLogin](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)、[BlackboxSign README](https://github.com/ypq123456789/BlackboxSign) |
| `hkey` + `_time` + `nonce` | **web 读签名三件套**(查询参数,非 header),每次请求重算,只签路径不签 query | feeds、topic/feeds、link/tree、search/v1、share/data 等 | [heybox-cli signer.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/signer.go) 注释:"It only signs the canonical endpoint path" |
| `_rnd` | **web 写签名**(`16:` 前缀 + AES-256-GCM base64url,绑定时间/nonce/路径,网页包内公开 32 字节密钥常量);App 原生侧 `_rnd` 由 SO 计算 | 写接口(评论等);readscope 不需要 | [astrbot 契约](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md) |
| `imei` / `device_id` / `device_info` / `os_type` / `os_version` / `version` / `x_client_type` / `x_app` / `x_os_type` / `web_version` | 设备与环境伪装参数。App 侧要 `imei+os_version`;web 侧只要 `device_id`(可随机 32 hex)+ `device_info=Chrome` 等;`version` 用 web 假版本号 `999.0.3/999.0.4` | App 路线全带;web 路线带 web 公参即可 | [WenfuRainbow client.ts](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts)、[SCUTJcfeng client.py](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py) |

**web `hkey` 算法(纯算法,Go 可直接实现)** —— 两种公开变体并存(详见 §5 矛盾记录):

- 变体 A(数字/24 字符表 `JKMNPQRTX1234OABCDFG56789H`):`nonce=md5(random).toUpperCase()` → `nonceDigits=仅数字(nonce+alphabet)` → `digits=md5(ts+1+canonicalPath+md5(nonceDigits)) 的前9位数字(不足补0)` → 按 24 进制取 5 个字符 → 末 4 字节做 GF(2⁸) 列混淆变换求 checksum%100 → `hkey=5字符+%02d`。Go 实现:[heybox-cli/internal/api/signer.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/signer.go);同源 JS 实现:[RSSHub util.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/util.ts)(注明 "Copied from https://github.com/huandu/heybox-url")。
- 变体 B(32 字符表 `AB45STUVWZEFGJ6CH01D237IXYPQRKLMN89` + 交错拼接):时间/路径/nonce 分别按字符表映射 → interleave 取前 20 字符 → MD5 → 末 6 字符列混淆 checksum%100 + 前 5 位映射前缀。来源:[WenfuRainbow signature.ts](https://github.com/WenfuRainbow/heybox/blob/master/src/api/signature.ts);独立博客描述同一算法:[小黑盒 MCP:从抓包到 Native 签名的完整逆向](https://foxlesbiao.github.io/blog/posts/hermes/2026-10-23-xiaoheihe-mcp-signing-implementation/)(2026-10-23)。
- App 原生签名(JNI `libglesv3_1.so` + `libnative-lib.so`,Unidbg 模拟 + `/account/getui/fix` 下发的 `security_code/security_version`)只在走 App 协议时才需要;**readscope 走 web 协议,不需要**。来源:[foxlesbiao 博客](https://foxlesbiao.github.io/blog/posts/hermes/2026-10-23-xiaoheihe-mcp-signing-implementation/)。

---

## 3. 反爬/风控

1. **验证码**:`heybox-cli` 把 `show_captcha/need_captcha` 作为独立错误类别(退出码 4),并声明"CLI 不会尝试绕过验证码";`xiaoheihe-mcp` README:"通用帖子搜索偶发会触发平台验证码;此时请改用 topic_search/topic_posts 或 game_search,稍后再试"。来源:[heybox-cli README](https://github.com/shadowfish07/heybox-cli)、[xiaoheihe-mcp README](https://github.com/foxlesbiao/xiaoheihe-mcp)(直接证据)。
2. **限流**:HTTP 429 与 `msg 含"频繁/限流"` 均被 heybox-cli 单独分类;AstrBot 建议持续 429 时"增大轮询间隔并减少并发";heybox-cli 翻页时"页间有短暂延时以降低触发限流的概率",`--all` 默认上限 5 页/最多 20 页。来源:[heybox-cli client.go/README](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)、[astrbot README 风险段](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter)(直接证据)。
3. **版本门槛**:RSSHub issue #13686(2023-11):`/xiaoheihe/news` 返回 `{"status":"failed","msg":"您的版本过低,请您更新版本"}` —— 服务端校验 `version` 参数。来源:[RSSHub#13686](https://github.com/DIYgod/RSSHub/issues/13686)(直接证据)。
4. **登录态失效**:AstrBot:"遇到 `relogin`/401 重新扫码";heybox-cli:`401/403` 归类为需要登录。来源:[astrbot README](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter)、[heybox-cli client.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)。
5. **失效历史(RSSHub 时间线,约一年一坏、可修)**:#11511(2022-12 news 失效)→ #13686(2023-11 版本门槛)→ PR #14764(2024-03 重写路由,+312/-179,沿用至今)→ #17482(2024-11 discount 失败,已关闭)→ #18439(2025-02 add2cart 失败,已关闭)。来源:[#11511](https://github.com/DIYgod/RSSHub/issues/11511)、[#13686](https://github.com/DIYgod/RSSHub/issues/13686)、[PR#14764](https://github.com/DIYgod/RSSHub/pull/14764)、[#17482](https://github.com/DIYgod/RSSHub/issues/17482)、[#18439](https://github.com/DIYgod/RSSHub/issues/18439)(直接证据)。
6. **封号风险信号(社区自述,非官方数据)**:`pyxiaoheihe` README:"使用第三方客户端有封号风险,后果自负";`HeiDaotu/ScriptDocument`:"`imei` 可以不填,但有可能会封号";`heybox-core`:"建议轮询间隔 ≥ 10 秒,避免封号";`BetterHeybox`(LSPosed):"可能存在账号封禁、数据异常等风险"。来源:[chr233/pyxiaoheihe](https://github.com/chr233/pyxiaoheihe)、[HeiDaotu/ScriptDocument xiaoheihe 文档](https://github.com/HeiDaotu/ScriptDocument/blob/main/docs/reference/xiaoheihe/index.md)、[XiaHouSheng/heybox-core](https://github.com/XiaHouSheng/heybox-core)、[Mrmiaomrzh/BetterHeybox](https://github.com/Mrmiaomrzh/BetterHeybox)(直接引文)。**研究者解读**:这些警告多来自签到/刷任务/自动评论等"写密集"场景;低频只读浏览的风险显著更低,但没有官方豁免依据。
7. **风控基建(官方侧)**:《隐私政策》5.7:"我们会收集您的设备信息并通过第三方SDK(**数美SDK**)进行风控验证"。来源:[小黑盒隐私政策](https://api.xiaoheihe.cn/account/privacy_introduce/)(直接证据)。
8. **最近一年仍活着的项目**(2026 年有提交/发布):`heybox-cli`(Go,2026-07 起)、`WenfuRainbow/heybox`(2026-07~09)、`xiaoheihe-mcp`(2026-10)+ 活跃 fork `huajiao1998/xiaoheihe-mcp`(2026-11-12)、`astrbot_plugin_xiaoheihe_adapter`(2026-11-12 push)、`AaronZ345/xiaoheihe-cli`(契约 2026-10-10)、`BlackboxSign`、`heybox-daily`(2026-11-12)、`GUAPIXIA/xiaoheihehe` 第三方 Windows 客户端(2026-11-11)、DSH 插件系(`plasta2steel`、`fbbDirge`(2026-11-13)、`lumingfei004/dph-heybox`)。GitHub 搜索快照见 §2.1。

---

## 4. 免登录可达面 & 登录态获取

### 4.1 不发登录态能拿什么

- **全站新闻/资讯流**:`/bbs/app/feeds/news`(`heybox_id=-1` + hkey,RSSHub 生产验证)。来源:[RSSHub news.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/news.ts)。
- **任意帖子正文**:`/bbs/app/api/share/data/`(`heybox_id=-1` + hkey;返回 `link.content[0].text`)。来源同上。
- **任意公开用户的资料与动态**:`/bbs/app/profile/user/profile`、`/bbs/app/profile/events`(RSSHub 生产,**连签名都不带**)。来源:[RSSHub user.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/user.ts)。
- **话题/游戏搜索**:`/bbs/app/api/search/topic`、`/game/search/`(heybox-cli 中 `signed=false`)。来源:[heybox-cli client.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go)。
- **推荐流/话题流/帖子树/楼中楼**:MCP 声明"公开浏览默认无需登录"(`XIAOHEIHE_NATIVE_SIGNER=0` 时"公开接口回退无签名请求"),heybox-cli 的 post/comments 命令匿名可跑(登录只影响 search);但 WenfuRainbow/AstrBot 出于产品形态全部走登录态。来源:[xiaoheihe-mcp README](https://github.com/foxlesbiao/xiaoheihe-mcp)、[heybox-cli README](https://github.com/shadowfish07/heybox-cli)。

### 4.2 发登录态(cookie `pkey`)能多拿什么

- 关注的圈(真实分区)目录 + 分区流:`/bbs/app/api/topic/index/` + `/bbs/app/topic/feeds`(AstrBot 主用);`/bbs/app/topic/categories` + 同一分区流(WenfuRainbow)。
- 关注的人的动态:App 侧 `/bbs/app/link/followed/post` + `/bbs/app/api/follow/alert`(2020 端点,现况未复核);web 侧可用 `/bbs/app/profile/following/list` 逐个拉 `/bbs/app/profile/events`(免登录可用的用户动态端点)拼接。
- 消息/通知:`/bbs/app/user/message`(@、回复、获赞、关注)、官方消息、折扣消息。
- 收藏:`/bbs/app/profile/fav/folder/v2/links`。
- 全站搜索 `/bbs/app/api/general/search/v1`(heybox-cli:`lack_token` → 需登录)。

### 4.3 登录态获取方式(由省到繁)

1. **Web 扫码登录(推荐,Go 参考现成)**:`GET /account/get_qrcode_url/?app=web` → 终端渲染二维码 → 轮询 `GET /account/qr_state/` → 成功响应直接给 `heybox_id` + `pkey`。heybox-cli 全套 Go 实现(还处理 `need_google_check` 二次验证回退浏览器)。来源:[heybox-cli client.go](https://github.com/shadowfish07/heybox-cli/blob/main/internal/api/client.go) + README(直接证据)。会话字段只存 `heybox_id/pkey` 等,0600 文件 —— 与 readscope 知乎 M4 的 0600 cookie 文件契约完全同构。
2. **F12 贴 cookie**:登录 `www.xiaoheihe.cn` 后复制 cookie(`pkey`、`heybox_id`、`x_xhh_tokenid`)。VSCode 插件、DSH 插件都支持手动粘贴;`dsh-xiaoheihe` 的表述("账号态与接口都需要 Cookie 与 hkey 签名")说明 cookie 提供身份、hkey 每次本地计算。来源:[WenfuRainbow client.ts loadConfig](https://github.com/WenfuRainbow/heybox/blob/master/src/api/client.ts)、[plasta2steel/dsh-xiaoheihe](https://github.com/plasta2steel/dsh-xiaoheihe)。
3. **App 抓包**:Charles/mitmproxy 抓 `api.xiaoheihe.cn`,取 `heybox_id/pkey/imei`(免 root 教程见 Chr_ 博客);MCP 支持"从真机 HAR 导入"(要求 HAR 含成功的 `POST /account/login_code/` 响应)。来源:[Chr_ 教程](https://blog.chrxw.com/archives/2020/08/10/1353.html)、[xiaoheihe-mcp README](https://github.com/foxlesbiao/xiaoheihe-mcp)。readscope 不需要走这条(除非要 App 侧原生端点)。

### 4.4 本轮实测(直接证据,附环境局限)

在本调研沙箱内,对 RSSHub 免签名用户端点发起裸 GET(不带签名、不带登录、非浏览器 UA):
`GET https://api.xiaoheihe.cn/bbs/app/profile/user/profile?lang=zh-cn&version=1.3.303&userid=30664023`
响应体:

```json
{"msg":"请求失败了","result":{},"status":"failed"}
```

即:服务端**没有硬拒(非 403/封 IP),但业务层失败**。可能原因是 UA/Referer 头缺失、`version=1.3.303` 已过期(issue #13686 型版本门槛)或数据中心 IP 风控。**研究者推论**:RSSHub 生产环境可达性 ≠ 任意环境可达性;readscope 落地时第一步应做冒烟测试,带上 web 公参 + 浏览器 UA + `Referer: https://www.xiaoheihe.cn/` 再判断。本轮无代码执行能力,无法现场生成 hkey 验证签名端点,这是本报告最主要的验证缺口。

---

## 5. 内容形态 & 与 readscope 渲染管线的兼容性

> 编号说明:本报告 §5=内容形态、§6=矛盾记录、§7=法务、§8=缺失证据(前文 §3 反爬、§4 免登录面已用)。

数据模型(来源:[WenfuRainbow src/types/index.ts](https://github.com/WenfuRainbow/heybox/blob/master/src/types/index.ts),直接证据;与 AaronZ345 契约、RSSHub 解析互相印证):

- **帖子 `PostLink`**:`linkid`、`title`、`description`(摘要)、**`text`(正文,"可能是纯文本或 JSON 格式的富文本块")**、`user`(含等级/勋章)、`topics[]`、`up/down/favour_count/comment_num`、`create_at/modify_at`(秒级时间戳)、`ip_location`、`content_type/link_type/link_tag`、`share_url`、`pics?: [{url,width,height}]`、`has_video`。
- **正文富文本**:短评/动态正文是文本块数组序列化 JSON,AaronZ345 契约给出原语:纯文本短评正文 = `[{"type":"text","text":"正文"}]`;文章类(`分类 11`)是"HTML 保留图片位置,同时提供图片元信息块";`/bbs/app/api/share/data/` 的正文取 `link.content[0].text`。来源:[AaronZ345 契约](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md)、[RSSHub user.ts](https://github.com/DIYgod/RSSHub/blob/master/lib/routes/xiaoheihe/user.ts)(直接证据)。
- **评论 `Comment`**:`commentid`、`text`(纯文本)、`child_num`(楼中楼总数)、`up/down`、`floor_num`(楼层号)、`create_at`、`ip_location`、`user`、`replyuser`(被回复者,顶级为 null)、`imgs[]`(评论图,`url/thumb/width/height`)。组织形式:`comments: CommentGroup[]`(根楼层 + 子回复),`has_more_floors` 翻页标志。
- **投票(poll)**:抓取的所有类型定义与契约文档里**均未出现投票字段**;未验证(见 §8)。
- **转发帖**:`link_type/link_tag/content_type` 数值区分帖子类型(图文 27 / 动态 28 / 文章 11),`share_url` 与 `src link` 类型的语义未逐项验证;VSCode 插件把"转发/分享帖"统一当 link 渲染(其 UI 支持"正文、楼层、嵌套回复和原图")。
- **图片**:展示图可通过 `/bbs/app/api/original/image` 换原图;`imgs` 字段"在不同版本中可能是 URL 字符串或 JSON 字符串"(WenfuRainbow 代码注释,需容错解析)。

**与 readscope「HTML→行模型」管线的兼容性(研究者解读)**:
- `text` 的 JSON 文本块 → 直接映射行模型,比 HTML 更简单;文章类的 HTML 片段可复用现有 HTML→行模型管线。
- `pics[]`/`imgs[]` → 图片 URL 列表,与 HN/知乎图片处理同构(占位行 + 可选下载);`has_video` 帖子降级为"标题 + 视频标记 + share_url 外链"即可。
- 评论树两层(根楼层/楼中楼)与知乎评论结构类似,`floor_num`/`replyuser` 天然适合缩进行模型。
- 总体:**兼容性风险低**,难点只在 `imgs` 字段类型漂移和文章 HTML 的样式噪声,均为解析层小工作量。

---

## 6. 矛盾记录(未消解,按证据并存)

1. **web `hkey` 两种算法并存且都自称"当前"**:`heybox-cli`(Go,2026-07,字母表 24 字符数字进制)与 `WenfuRainbow/heybox`/foxlesbiao 博客(2026-07~10,字母表 32 字符 interleave)。可能解释:服务端同时接受新旧两代 web 签名,或不同端点组要求不同算法;两仓库各自在其实现范围内工作。**本轮无法实测裁决**。
2. **`/bbs/app/feeds`、`/bbs/app/topic/feeds` 是否需登录**:AstrBot 契约表标"认证=是",而 MCP("公开浏览默认无需登录")与 heybox-cli(登录只影响 search)暗示匿名可用。可能是 AstrBot 客户端设计上恒带 cookie,而非服务端强制。
3. **`huandu/heybox-url` 声明废弃 vs RSSHub 生产沿用其算法**:README 说"doesn't work with latest Heybox API",但 RSSHub 2024-03 重写后沿用至今且路由存活。与矛盾 1 同源(算法版本/端点差异)。

---

## 7. 法务/条款

**小黑盒《使用协议》(Steam 上架副本,即 App EULA 所载同一使用协议文本)关键条款原文**:

> "4.3.12 未经小黑盒事先明确书面许可,以任何方式(包括但不限于**机器人软件、蜘蛛软件、爬虫软件等任何自动程序、脚本、软件**)和任何理由自行或委托他人、协助他人获取平台的服务、内容、数据;"
>
> "1.2 ……任何以**恶意破解等非法手段**将小黑盒所提供的服务与小黑盒平台分离的行为,皆不属于本协议约定的由小黑盒提供的服务。"
>
> "6.2 用户不得修改、改编、翻译小黑盒服务所使用的软件、技术、材料等……**不得通过反向工程、反编译、反汇编或其他类似行为获得其的源代码**……"

来源:[小黑盒使用协议和隐私条款(Steam EULA 副本)](https://store.steampowered.com/eula/1447430_eula_0)(直接引文;注:这是 Steam 商店托管的协议副本,App 内最新版措辞可能微调,本轮未能在站内直接抓到使用协议正文页,`api.xiaoheihe.cn/account/privacy_introduce/` 抓到的是《隐私政策》部分)。另有《隐私政策》5.7 条披露使用数美 SDK 做设备风控:[隐私政策](https://api.xiaoheihe.cn/account/privacy_introduce/)。

**与知乎 M4 路线的对比(研究者解读,非来源直述)**:
- 相同点:两者都是"免登录可达面有限 + 手动贴 cookie 解锁个人数据面 + 私有 JSON API + 个人自用低频只读"。知乎服务条款同样有禁止自动化抓取的条款(readscope M4 已接受该风险),小黑盒 4.3.12 的表述同类且更直白地点名"自动程序/脚本"。
- 差异点/加重点:① 小黑盒多了"反向工程禁止"(6.2)——但 readscope 走的 web `hkey` 是社区已公开复刻的纯算法,不需要反编译官方客户端;若哪天要上 App 原生签名(Unidbg/解包 SO),就明确撞上 6.2,应视为红线。② 小黑盒有明确的风控 SDK 与封号先例警告(§3.6),写操作风险尤其高。③ 单机、单用户、只读、限频(页间延时、日配额)是可执行的低风险姿态;RSSHub 这种公开多用户聚合实例多年存活,说明只读低强度访问未被系统性封禁(存在幸存者偏差,注意)。
- 结论:**风险与知乎 M4 同量级**,前提是只读 + 手动 cookie + 限频;不建议做点赞/评论/签到任何写功能。

---

## 8. 缺失证据 / 未验证

- **`nonebot-plugin-hbrother`(确定性否定)**:PyPI 404;GitHub 全站搜 `hbrother` 仅 6 个仓库且全部无关(服装公司官网、清关公司系统、个人主页等);NoneBot Registry(registry.nonebot.dev)无此插件。结论:该插件已从公开渠道消失(下架/改名/从未公开发布)。任务提示的鉴权字段"heybox_uid+pkey+hkey"与实际生态字段(`heybox_id`/`pkey`/`hkey`)有一处命名出入,无法核实其原始实现。来源:[GitHub search API `q=hbrother`](https://api.github.com/search/repositories?q=hbrother)、[PyPI](https://pypi.org/project/nonebot-plugin-hbrother/)(404)、[NoneBot Registry](https://registry.nonebot.dev/)。
- **`ozline/heybox-go`**:VSCode 插件 README 提到的配套仓库,`api.github.com/repos/ozline/heybox-go` 返回 404(私有或未公开),无法作为 Go 参考证据。
- 投票/投票帖的 JSON 字段结构:所有抓取的类型定义与契约中均未出现。
- 转发帖(`link_type` 具体枚举值)语义、`game.xiaoheihe.cn` 页面可达性、`api.xiaoheihe.cn` 的 robots.txt/官方抓取政策页:未验证。
- 带签名的端点在本调研环境中的实际可用性(无代码执行能力,无法生成 hkey 实测);§4.4 的裸请求实测返回 `status=failed` 提示环境/参数敏感性。
- 精确限流阈值、`pkey` 有效期时长(仅知扫码响应含 `expire_at` 字段):无公开数据。
- 站内最新版《使用协议》正文页 URL(本轮抓到隐私政策与 Steam EULA 副本,两者条款编号一致度高但以 Steam 副本为引用源)。

---

## 9. 结论与建议

### 可行性评级:**能做(只读、web 协议、可选手动 cookie/扫码)**

硬约束逐条对照:
- 不需要浏览器常驻通道:签名是纯 MD5/字符表算法,Go 原生实现(heybox-cli `signer.go` 为 MIT 许可,可移植/参考重写)。
- 不需要 JS 签名器:与知乎 `x-zse-96` 类不同,web `hkey` 无需 JS 运行时。(注:`heybox-core` 用 Bun 跑 JS 签名是它的实现选择,不是必需。)
- 不需要抓包逆向 App 二进制:web 端点 + web 公参即可覆盖列表/详情/评论三层;App 原生(Unidbg)路线仅当未来 web 端点全部要求原生签名时才需要,当前无此迹象。

### 推荐数据面(三层)

| readscope 层 | 首选端点 | 兜底/备选 |
|---|---|---|
| 列表(圈子/板块帖子流) | `GET /bbs/app/topic/feeds`(`topic_id` 来自 `/bbs/app/api/topic/index/` 或 `/bbs/app/topic/categories`) | `GET /bbs/app/feeds`(推荐流)、`GET /bbs/app/feeds/news`(免登录新闻流) |
| 列表(关注动态,登录态) | `/bbs/app/profile/following/list` → 每人 `/bbs/app/profile/events`(免登录端点) | App 侧 `/bbs/app/link/followed/post`(pkey,老端点需复核) |
| 详情(正文) | `GET /bbs/app/link/tree`(`link.text` 文本块) | `GET /bbs/app/api/share/data/`(免登录,`content[0].text`) |
| 评论 | `GET /bbs/app/link/tree`(根楼层分页,`has_more_floors`) + `GET /bbs/app/comment/sub/comments`(`root_comment_id`+`lastval` 游标) | 排序参数 `sort_filter=hot|latest` |
| 登录(可选) | 扫码 `GET /account/get_qrcode_url/?app=web` + `GET /account/qr_state/`;凭据存 0600 文件(对齐 M4 契约) | F12 手动贴 `pkey` cookie |
| 明确不做 | 写操作(评论/点赞/收藏,需 `_rnd` AES-GCM 写签名且封号风险高)、App 原生签名(Unidbg)、绕验证码 | — |

### 工程量预估(相对知乎 M4)

**≈ M4 或略小**。理由(研究者判断):
1. 签名器:移植/重写 `signer.go`(~110 行,含测试向量可对照 RSSHub JS 版交叉验证)——M4 知乎靠"免签 API + cookie"绕开了签名器,这里多一个约半天~1 天的签名模块,但换来的是端点比知乎稳(无 `x-zse-96` 轮换史,签名算法 2021 年至今基本形态未变,只在 2026 年出现第二变体并行)。
2. 三层端点 + 数据模型:类型定义可直接照 [WenfuRainbow types/index.ts](https://github.com/WenfuRainbow/heybox/blob/master/src/types/index.ts) 翻成 Go struct;`text` JSON 块 → 行模型比 HTML 解析更省。
3. 登录态:扫码登录是增量(M4 没有),heybox-cli 有完整 Go 参考可抄实现思路;也可以 M5 先只做"F12 贴 pkey"再补扫码。
4. 维护成本:预留 `version`/`web_version` 参数与签名变体 A/B 的开关,应对"版本过低"式失效(RSSHub 的历史失效大多是这个量级的小修)。

### 落地前必做的冒烟清单(弥补本轮无法实测的缺口)

1. 带浏览器 UA + `Referer: https://www.xiaoheihe.cn/` + web 公参 + 变体 A 签名,依次打 `/bbs/app/feeds/news`(免登录)、`/bbs/app/link/tree`、`/bbs/app/topic/feeds`;`status!=ok` 时读 `msg` 判断是版本门槛还是风控。
2. 变体 A 失败则切换变体 B(32 字符表)再试;两种算法的服务端接受范围是本报告最大的未决问题(§6.1)。
3. 若裸 IP 被风控(持续 `failed`),确认是否需要住宅 IP 或登录态才可达 —— 这决定 M5 免登录层的成色。

---

## 10. 来源清单

**保留(核心佐证)**:
- [shadowfish07/heybox-cli](https://github.com/shadowfish07/heybox-cli)(Go 只读 CLI;`internal/api/signer.go` Go 签名实现、`client.go` 端点/错误分类/扫码登录)
- [WenfuRainbow/heybox](https://github.com/WenfuRainbow/heybox)(VSCode 插件;`src/api/client.ts`、`src/api/signature.ts`、`src/types/index.ts` 数据模型)
- [DIYgod/RSSHub · lib/routes/xiaoheihe](https://github.com/DIYgod/RSSHub/tree/master/lib/routes/xiaoheihe)(`util.ts` hkey 算法、`news.ts`/`user.ts` 免登录生产路由;issues #11511/#13686/#17482/#18439、PR #14764 失效时间线)
- [674537331/astrbot_plugin_xiaoheihe_adapter · docs/xiaoheihe-api-contract.md](https://github.com/674537331/astrbot_plugin_xiaoheihe_adapter/blob/main/docs/xiaoheihe-api-contract.md)(最严谨的端点契约与 `_rnd` 写签名、风控建议)
- [AaronZ345/xiaoheihe-cli · docs/api-contract.md](https://github.com/AaronZ345/xiaoheihe-cli/blob/main/docs/api-contract.md)(2026-10-10 核对的 web 读写契约、分页游标、正文 JSON 块)
- [foxlesbiao/xiaoheihe-mcp](https://github.com/foxlesbiao/xiaoheihe-mcp) + [签名逆向博客](https://foxlesbiao.github.io/blog/posts/hermes/2026-10-23-xiaoheihe-mcp-signing-implementation/)(web/native 双签名体系、验证码现象)
- [小黑盒使用协议和隐私条款(Steam EULA 副本)](https://store.steampowered.com/eula/1447430_eula_0)(4.3.12/1.2/6.2 条款原文)
- [小黑盒隐私政策](https://api.xiaoheihe.cn/account/privacy_introduce/)(数美 SDK 风控)
- [什么是小程序工坊 | @heybox/hb-sdk](https://docs.xiaoheihe.cn/hb_sdk/guide/)、[bot.xiaoheihe.cn 协议](https://bot.xiaoheihe.cn/protocol/bot)(官方开放平台边界)
- [XiaHouSheng/heybox-core](https://github.com/XiaHouSheng/heybox-core)、[HadeonYu/heybox-bot](https://github.com/HadeonYu/heybox-bot)、[chr233/pyxiaoheihe](https://github.com/chr233/pyxiaoheihe)、[SCUTJcfeng/xiaoheihe client.py](https://github.com/SCUTJcfeng/xiaoheihe/blob/master/client.py)、[ypq123456789/BlackboxSign](https://github.com/ypq123456789/BlackboxSign)、[Cansiny0320/heybox-daily](https://github.com/Cansiny0320/heybox-daily)、[huandu/heybox-url](https://github.com/huandu/heybox-url)、[Chr_ 逆向系列](https://blog.chrxw.com/archives/2020/08/05/1347.html)(生态矩阵、历史算法、封号风险警告)
- [GitHub search API 快照](https://api.github.com/search/repositories?q=xiaoheihe&sort=updated)(91 仓库、2026-11 活跃度证据;`q=hbrother` 否定性证据)
- [k1m0206/better-XiaoHeiHe](https://github.com/k1m0206/better-XiaoHeiHe)(web 页面路径与接口佐证)、[plasta2steel/dsh-xiaoheihe](https://github.com/plasta2steel/dsh-xiaoheihe)、[lumingfei004/dph-heybox](https://github.com/lumingfei004/dph-heybox)、[GUAPIXIA/xiaoheihehe](https://github.com/GUAPIXIA/xiaoheihehe)(cookie+hkey 直连与第三方客户端佐证)

**降权/排除**:
- 各 SEO 站点/聚合转载的"小黑盒 API"文章 —— 无源码佐证,未采用。
- `nonebot-plugin-shiro-personification`、`nonebot-plugin-parser(-lite)` 等仅因关键词命中,与小黑盒无关。
- `gehongyan/HeyBox.Net`、`QingFengOpen/HeychatDemo` —— 黑盒语音(聊天)平台 SDK,非社区内容 API,仅用于界定官方开放边界。
- `Mrmiaomrzh/BetterHeybox`/`HeyboxAPK` —— LSPosed 模块与 APK 归档,佐证封号风险表述,不作为数据面参考。
- `ozline/heybox-go` —— 仓库 404,无法核验,不采信 VSCode 插件 README 的转述。

---

*报告完。由 readscope M5 调研子任务产出;所有 GitHub 文件引用均在本轮实际抓取原文核对,未转述 README 之外的第二手信息(除明确标注"研究者解读/推论"处)。*
