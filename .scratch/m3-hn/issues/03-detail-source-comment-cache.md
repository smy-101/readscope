# 03: DetailSource 扩展 + 评论整树缓存

**What to build:** 源缝新增可选扩展 **DetailSource**(开帖详情按需取数,与 ConditionalSource 同款"可选扩展"模式);hn 实现:调 Algolia items 端点**一次取整棵评论树**,按条目整树写入 comments 表(整树替换)。此票交付数据层与缓存语义,UI 仍是正文层——验证靠测试与缓存内容。收藏快照只存帖子本身。

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] DetailSource 可选接口落地;未实现它的源(rss)不受任何影响
- [ ] hn Detail 一次请求返回全树;comments 按条目整树写入,parent 关系完整
- [ ] 重复 Detail 同一帖子不产生重复行(整树替换语义)
- [ ] 开过的帖子,评论离线可读(从缓存读出全树)
- [ ] 收藏快照 = 帖子(标题/链接/正文),不含评论树
- [ ] httptest fixture:深嵌套树 / 删除占位(deleted) / 空树 / 纯文本评论,各一例,离线可跑
