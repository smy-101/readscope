# 03: 帖子两层详情 + 评论层

**What to build:** 在推荐流/圈流按 Enter 打开帖子卡,进入文章卡同构的两层详情:帖子层渲染正文全文(纯文本块 + `[cube_X]`→`[X]` 降级、图片 `[图片]` 链接标记、视频帖标题+「视频」标记、文章帖 HTML 走渲染管线),页脚「N 条评论」;再 Enter 进评论层——根评论成楼(楼层头 author · 赞 · N 条回复)、楼中楼扁平化两层带 @parent,child_num 缺口按 lastval 游标自动补全(缺口计算 + 去重 + 游标终止,评估复用知乎评论补全的纯函数),Ctrl+N 按楼层 offset/total_page 续拉。开帖标记区分「开过的 0 评论帖」与「从未打开」;评论缓存离线可读;Esc 逐层退、各层滚动独立。收藏帖子卡为收藏快照;Ctrl+O/Ctrl+Y 走 share_url。详情拉取用 hex link_id(十进制会触发 captcha,冒烟已证)。

**Blocked by:** 01(列表条目形状与传输层)。与 02 并行,互不阻塞。

**Status:** done (2026-11-13,agent 侧;真网 smoke 与手感查走 04 号票)

- [x] Enter 帖子卡 → 帖子层正文全文;Esc 退回列表,滚动位置保留
- [x] cube 表情降级为 `[表情名]`;图片出 `[图片]` 链接标记;视频帖不空屏不卡渲染
- [x] 文章帖(is_article,HTML 正文)经渲染管线排版阅读
- [x] 帖子层页脚「N 条评论」,Enter 进评论层;0 评论帖直接提示,零请求
- [x] 根评论成楼、楼中楼扁平化带 @parent,与知乎评论层同观感
- [x] 高楼层帖(50+ 楼)Ctrl+N 续拉至 total_page 尽头;楼中楼 child_num 与可见子回复数一致
- [x] 已开过的帖子重开渲染缓存零请求、断网可读;开帖标记区分空评论开过帖
- [x] 收藏为快照,原帖字段全存;Ctrl+O/Ctrl+Y 正确
- [x] detail 拉取失败(风控/网络)状态栏提示,不崩溃不清缓存
- [x] 降级规则、楼层分页、楼中楼补全(缺口/去重/游标终止)有纯函数或 httptest fixtures 测试;UI 按键泵覆盖两层导航

## Comments

- **2026-11-13 实现完成**(CI 恒离线;真网验证归 04 票)。落点:
  - **详情拉取**:`DetailSource` + `DetailPager` 落在 heybox.Source 上;详情键 = 条目 id 里的 hex link_id(十进制是 captcha 蜜罐,冒烟已证);`/bbs/app/link/tree`(offset/limit=20/total_page 翻页)单次返回正文 + 首页楼层(含楼中楼热预览),`/bbs/app/comment/sub/comments`(root_comment_id/lastval/limit)游标翻楼中楼。
  - **正文管线**:`postBody` 纯函数处理三态 link.text——纯文本(cube 降级)、JSON 文本块数组(text 块拼段、带 URL 块收图)、文章帖 HTML 直通渲染管线;链接级 imgs 容错解析(对象/字符串两版形状,坏条目跳过不fail整树);有图或有视频时组装 HTML(`<img>`→管线 `[图片]` 链接标记;视频追加 `[视频]` share_url 链接行),空 text 返回空串保住列表摘要快照。
  - **楼层/楼中楼**:根评论 ParentID=""、热预览按 replyid 真实挂链(子回复的子回复保留真父,扁平化 @parent 靠它);child_num 原样入 NumChildren(零也存,零请求拒入靠它);缺口计算(child_num - 已载预览)> 0 的楼串行走 sub/comments(去重热预览、has_more/lastval 终止、每楼上限 6 页),孤儿 replyid 重挂本楼防 UI 提层;串行而非并发——拨号账本按 dial 记账,并发 walker 会在同一次醒来的同一时刻挤在一起(冒烟零 429 靠的是串行间隙)。
  - **refresh 合并层**:`mergeDetailTree` 新鲜楼层自带子树时优先新子树(否则保缓存子树,知乎形状不变);`appendFloors` 新楼层的内联预览随楼落地(旧楼晚到的子回复丢弃,归全量刷新的 merge 接,注释已论证)。
  - **UI**:`post` 旗标(与知乎 `article` 分立:同是「正文层无内联楼层表」,但 Enter 进楼层而非伪楼回复层);页脚计数到服务器 comment_num(列表热度快照,与 web 一致),无快照回退已载数;楼层层复用 pager 楼层头(作者 · 赞 N · N 条评论)与 ←/→/Ctrl+N hop-then-fetch;新文案 heybox.floors_loaded/floors_tail 双语;detailErrStatus 映射风控族 → 稍后再试(与列表同糖衣)。
  - **CommentURL 返回 ""**:裸 commentid 没有自己的网页(share 页属于帖子),heybox 是 DetailPager 源,Ctrl+O/Ctrl+Y 全走 AnswerURL = share_url(票面要求)。
- 待 04 票:真样本核验转发帖/文章帖/图片帖渲染、100+ 楼真网续拉、imgs 实际形状校准、楼中楼真实嵌套深度。
