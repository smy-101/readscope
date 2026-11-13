# M5 小黑盒 smoke 记录(2026-11-13,agent 侧)

> 环境:WSL2 Arch(住宅网络,非数据中心 IP),Go 1.27.3,桌面 Chrome UA + `Referer: https://www.xiaoheihe.cn/`。
> 方法:两变体签名器先用 node 跑原版 JS(RSSHub `util.ts` / WenfuRainbow `signature.ts`)生成 4 组参考向量做一致性自检(全过),再发真实请求;全部原始响应在 `smoke/samples/` 与 `smoke/samples-probe/`,复现脚本 `smoke/main.go`(全链)与 `smoke/probe/main.go`(参数矩阵)。
> 上游调研:`research.md`(本轮修订其 §6.1 / §9,见文末)。

## 结论(TL;DR)

**社区查看三层(列表/详情/评论)免登录全部可达,零浏览器、零登录、纯 Go HTTP + 纯算法签名。** 可行性从"能做"升格为"已验证"。

## 结果矩阵

| # | 端点 | 参数形态 | 签名 | 结果 |
|---|---|---|---|---|
| 01 | `/bbs/app/feeds/news` | RSSHub 全参(`heybox_id=-1`, appid=900018355) | A | ✅ ok 78KB(22 条,1 广告卡) |
| 02 | 同上 | 同上 | B | ❌ 非法请求 |
| 03 | 同上 | 同上 | 无签名 | ❌ `缺少必要参数[_time]`(hkey 硬校验) |
| 04 | `/bbs/app/link/tree` | 精简参 + **十进制** link_id | A | ❌ show_captcha(多帖复现) |
| 05 | 同上 | 全参 999.0.4 | B | ❌ show_captcha |
| P3/P4 | 同上 | A+999.0.4 / B+999.0.3 交叉 | — | ❌ 非法请求(**签名-版本绑定**) |
| **P11** | `/bbs/app/link/tree` | 精简参 + **hex link_id**(取自 share_url) | A | ✅ **ok 27.7KB**(正文+9 楼根评论) |
| 06/P6-P10 | `/bbs/app/api/share/data/` | 全部组合(含 hex id) | A/B | ❌ `错误的帖子[1]`——该端点(对本环境)已死 |
| 07 | `/bbs/app/api/search/topic` | `q/offset/limit` | 无签名 | ✅ ok(拿到 topic_id=568547 黑神话圈) |
| 08 | `/bbs/app/topic/feeds` | 精简参 + topic_id/offset/limit | A | ✅ ok 97-115KB(20 帖/页) |
| P15 | 同上 offset=1 | 同上 | A | ✅ ok,与第 1 页零重叠(offset 翻页有效;`lastval` 返回空) |
| P14 | `/bbs/app/feeds`(推荐流) | 精简参 + `heybox_id=-1/pull=0/dw=800` | A | ✅ ok 73KB(10 帖,无广告卡) |
| 11 | `/bbs/app/profile/events` | RSSHub 免签形态(version=1.3.303) | 无 | ❌ `请求失败了`(**RSSHub 生产形态在本环境失效**) |
| 11b | 同上 | 精简参 + `heybox_id=-1` + userid/list_type | A | ✅ ok 136KB(任意公开用户动态) |
| P12/P13 | `/bbs/app/comment/sub/comments` | root_comment_id + link_id(**十进制/hex 均可**) | A | ✅ ok(4 条楼中楼,`lastval` 游标 + `has_more`) |

## 关键发现

1. **签名两变体并存且按 `version` 分派**:A↔`999.0.3`(RSSHub/heybox-cli 血统)、B↔`999.0.4`(现网 web 客户端血统);交叉组合一律"非法请求"。两代签名服务端都认——research.md §6.1 矛盾裁决。**readscope 选 A + 999.0.3**(参考实现多、MIT Go 代码现成)。
2. **hex link_id 是详情层的钥匙**:`link/tree` 用十进制 id → `show_captcha`(换帖、加 Origin/Cookie、全参均复现);用 `share_url` 里的 12 位混淆 hex id → 正常返回。hex↔十进制非简单 XOR(抽样 20 对,xor 全不同),不可本地换算——但**列表条目自带 share_url**,详情层键从列表层免费获得。`sub/comments` 两种形态都收。
3. **免登录可达面比预期大**:新闻流/推荐流/话题流/帖子树/楼中楼/任意用户动态全部匿名 OK。captcha 只出现在十进制 id 的 `link/tree`(风控蜜罐特征?)与登录端域(search/v1、消息、收藏)。
4. **share/data 已死**:全部 8 种组合"错误的帖子[1]",免登录详情兜底改由 hex-id link/tree 承担(它本来就是首选)。
5. **profile/events 免签形态失效**:RSSHub 生产路由的 `version=1.3.303` 无签名形态返回"请求失败了";加 web 公参 + A 签名后正常。readscope 直接走签名形态。
6. 话题搜索无需签名(heybox-cli 同观察),可当圈目录的免登录替代(`topic/index`、`topic/categories` 报告标记需登录,未测)。

## 数据面契约(冒烟验证版,修订 research.md §9)

基址 `https://api.xiaoheihe.cn`;公参精简集 = `os_type=web&version=999.0.3&x_app=heybox_website&x_client_type=web&x_os_type=Windows`(+ `heybox_id=-1`);签名 = query 参数 `hkey/_time/nonce`(变体 A,只签 canonical path)。

| readscope 层 | 端点 | 关键参数 | 备注 |
|---|---|---|---|
| 列表·话题流(主) | `GET /bbs/app/topic/feeds` | `topic_id/offset/limit` | offset 翻页;topic_id 来自搜索或目录 |
| 列表·推荐流 | `GET /bbs/app/feeds` | `offset/pull/dw` | 匿名即个性化(服务器按 device 指纹?) |
| 列表·新闻流 | `GET /bbs/app/feeds/news` | `offset/limit/appid` | 资讯向,广告卡需过滤 |
| 列表·用户动态 | `GET /bbs/app/profile/events` | `userid/list_type=moment` | 需签名形态 |
| 圈目录(免登录) | `GET /bbs/app/api/search/topic` | `q/offset/limit` | **无签名** |
| 详情+根评论 | `GET /bbs/app/link/tree` | **hex link_id**/offset/limit/sort_filter | `result.link.text` 正文;`comments[].comment[]` 楼层;`total_page/has_more_floors` 翻页 |
| 楼中楼 | `GET /bbs/app/comment/sub/comments` | `root_comment_id/lastval/limit` | `lastval` 游标 + `has_more` |
| hex id 来源 | 列表条目 `share_url` 的 `link_id=` | — | 不可本地换算,列表层透传 |

内容形态实勘:`text` 纯文本+`[cube_表情]` 标记;`imgs/thumbs` 图片数组;`has_video/video_url`;`ip_location` 在评论上有;评论 `replyuser`(被回复者)+ `user` 两套用户对象;表情 `[cube_滑稽]` 需降级渲染。

## 未测/后续(进 grill 前输入)

- **登录态**:扫码(`get_qrcode_url`/`qr_state`)与 cookie(`pkey`)未测——关注流/消息/收藏/全站搜索依赖它;heybox-cli 有全套 Go 参考。
- 圈目录登录形态(`topic/index?type=list`、`topic/categories`)未测;搜索可代。
- `link/tree` 的 `sort_filter=hot/latest`、`owner_only` 等参数未逐一测。
- 稳定性:单日冒烟,无长期失效率数据(RSSHub 时间线约一年一坏,见 research.md §3.5)。
- 频率:全程 ~30 请求、间隔 1.5-4s,零 429 零封禁;`show_captcha` 仅十进制 id 出现。
