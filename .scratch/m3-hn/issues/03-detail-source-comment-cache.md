# 03: DetailSource 扩展 + 评论整树缓存

**What to build:** 源缝新增可选扩展 **DetailSource**(开帖详情按需取数,与 ConditionalSource 同款"可选扩展"模式);hn 实现:调 Algolia items 端点**一次取整棵评论树**,按条目整树写入 comments 表(整树替换)。此票交付数据层与缓存语义,UI 仍是正文层——验证靠测试与缓存内容。收藏快照只存帖子本身。

**Blocked by:** 01

**Status:** ready-for-human

- [x] DetailSource 可选接口落地;未实现它的源(rss)不受任何影响
- [x] hn Detail 一次请求返回全树;comments 按条目整树写入,parent 关系完整
- [x] 重复 Detail 同一帖子不产生重复行(整树替换语义)
- [x] 开过的帖子,评论离线可读(从缓存读出全树)
- [x] 收藏快照 = 帖子(标题/链接/正文),不含评论树
- [x] httptest fixture:深嵌套树 / 删除占位(deleted) / 空树 / 纯文本评论,各一例,离线可跑

## Comments

**2025-12 实施**(待人工验收):

- 缝:`source.DetailSource`(与 ConditionalSource 同款探测式可选扩展);`core.Comment/Content`(列与 comments 表 1:1);组合层 `refresh.Detail`(拉取→整树入库,失败不动缓存);DAO `store.SaveComments/CommentsForItem`(单事务 delete+insert,树序=rowid 序,父先于子)
- 实测活 API 定形:评论节点 `points` 恒 null(楼层 score 无数据面,04 号票展示时需另想办法)、顶层评论 `parent_id`=story id(映射为父空)、Ask 正文在 `text` 字段(搜索端点叫 `story_text`);deleted 节点保留为空 author/text 占位,子代 parent 链不断
- 评论 id 存 Algolia 原始数字串(PK 为 (item_id,id),item_id 已限定作用域)
- "rss 不受影响"由既有全套 rss 测试照常通过钉住,另加 TestDetailRejectsNonDetailSource:探到非 DetailSource 是点名源的干净错误;未放"rss 类型不实现接口"的负面断言(测特性缺席,同义反复)
- UI 未动(票面约定仍为正文层);GLOSSARY 无新术语
