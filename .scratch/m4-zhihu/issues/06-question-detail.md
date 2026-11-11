# 06: 问题三层详情 — 回答楼层 / 回答层 / 评论层 + 缓存

**What to build:** 知乎问题走三层详情(HN 同构):**问题层** = 标题 + 元信息("提问人 · N 个回答 · N 万浏览")+ 描述(HTML 清洗进管线)+ 回答楼层列表(楼层头 author · 赞 N · N 条评论),数据 = v4/questions 元信息(带 x-zse-93)+ feeds 首批 5 条(不带 x-zse-93)两请求;**回答层** = 单个回答全文(←→ 在回答间移动),内容已随楼层拉取;**评论层** = 当前回答的评论,根评论各成一楼、子回复两层扁平化带 @parent——root_comment 每页 20 条(offset 翻页),子回复缺口 = child_comment_count − 已带,child_comment 游标补全(单根≤6 页,并发上限克制)。Ctrl+N 三层各自加载更多(楼层=feeds paging.next;评论=offset;回答层无翻页)。评论整树进 comments 缓存、开帖标记复用:二次打开先渲染缓存,在线后台刷新,离线可读。收藏快照=条目快照(标题/链接/描述),回答/评论走缓存语义不进快照。

**Blocked by:** 04

**Status:** ready-for-agent

- [ ] 问题层:元信息+描述+回答楼层;两请求编排,首批 5 条;楼层头三元组(author · 赞 N · N 条评论)
- [ ] 回答层:Enter 进入单答全文(清洗后管线排版),←→ 楼层间移动,Esc 逐层退,各层滚动状态独立保留
- [ ] Ctrl+N:楼层沿 feeds paging.next 追加(去重防排序抖动);到底判定含 answer_count 总数
- [ ] 评论层:root 楼层制 + 子回复两层扁平化 @parent;缺口补全纯函数(缺口计算/热门子回复去重/游标终止)单测覆盖
- [ ] 评论整树入 comments 缓存 + 开帖标记:二次打开先缓存后刷新,离线可读;楼层/回复层 Ctrl+N 内存分页
- [ ] v4 详情 401/403 触发 04 号票登录引导;404 = "问题不存在/已删除"的友好转译
- [ ] 内容清洗 golden:noscript/sticker/video-box(data-lens-id)/img 属性优先级/剥 /50/ 缩略代理/相邻同图合并
- [ ] httptest 假端点(元信息/feeds 多页/评论多页+子回复缺口 fixture)+ Model 按键泵四层导航测试,全部离线
