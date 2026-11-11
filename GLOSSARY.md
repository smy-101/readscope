# readscope

终端里的统一阅读器:RSS/HN 信息流 + 本地 EPUB 书库。本表收录项目语境里的领域术语。

## 信息流

**源 (Source)**:
一个内容来源,如 RSS、Hacker News。源是源栏(Tab)切换的单位。
_Avoid_: 站点、网站、平台

**Feed**:
源内固定的分类单位。RSS 的 feed 是一条订阅;HN 的 feed 是 Front/New/Ask HN/Show HN 四类。
_Avoid_: 订阅(泛指 RSS 行为时)、栏目

**条目 (Item)**:
列表里的一行内容单元,帖子或文章在 readscope 里的形态。

**未读点**:
条目行首的 ● 标记,表示该条目尚未打开过;打开即消,Ctrl+R 可恢复。
_Avoid_: 小圆点、蓝点

**收藏快照**:
收藏那一刻条目的完整存档;原帖从源里消失后快照仍可读。不含评论。

## Hacker News

**链接帖**:
正文指向外部文章的 HN 帖子;详情只显示一行文章链接加评论区。
_Avoid_: 外链帖、转帖

**文本帖**:
自带正文的 HN 帖子(Ask HN / Show HN);正文全文进渲染管线。

**热度快照**:
items 行上的 points 与评论数,取 fetched 时刻的值;列表的 "N pts · N comments · author" 热度雷达与 front/ask/show 的 points 序都读这里,刷新时随快照一起更新。RSS 行恒为空。
_Avoid_: 热度值、统计数据、实时热度

**楼层**:
帖子下的一条顶层评论;楼层头显示 author · score · N replies。
_Avoid_: 一楼/盖楼(口语)、顶层评论(冗长时)

**回复树**:
一条楼层的全部后代评论;Enter 进入查看。
_Avoid_: 子评论、评论串

**扁平化**:
回复树超过两层时的呈现规则:压成两层,跨层回复带 @parent 前缀,保留谁回复谁。
_Avoid_: 折叠(不同的概念)
