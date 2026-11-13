# 01: 传输层 + 推荐流贯通

**What to build:** 在 sources.toml 启用 `heybox` 后,源栏出现小黑盒,F5 刷出匿名推荐流并在终端可刷可读:传输层单点持有签名(变体 A,参考向量恒测)、web 公参(999.0.3 精简集)、安装时生成并持久复用的 device_id、风控哨兵(show_captcha/429/版本门槛 → 「稍后再试」,当次失败不清缓存)与页间延时;列表层产出帖子卡(hex link_id 主键、赞/评论热度快照、share_url 外链),广告卡(无 linkid)静默丢弃;Ctrl+N offset 翻页、已读/未读点、缓存先行离线可读、双语 UI 键齐备。

**Blocked by:** 无(可立即开始)。

**Status:** ready-for-agent

- [ ] 启用 heybox 后源栏出现小黑盒,推荐流 F5 拉取成功,条目显示标题/摘要/作者/赞数/评论数
- [ ] 签名器对 4 组参考向量(原版 JS 固定输入生成)恒测通过;签名只依赖 canonical path
- [ ] device_id 安装时随机生成 32hex 持久化,后续请求复用(重启不变)
- [ ] 广告卡不出现在列表;帖子卡 id 形如 `hb:link/<hexid>`,Ctrl+O 打开 share_url
- [ ] Ctrl+N 翻页命中前进、到底提示,重启后续翻不重拉
- [ ] show_captcha/429/版本过低 → 状态栏「稍后再试」级提示,不自动重试,已缓存内容不受影响
- [ ] 断网重启后列表即现、条目可读;页间延时可观察(无 429)
- [ ] 新增 UI 文案 zh-CN/en 双语齐备,语言包完整性测试过
- [ ] httptest 假小黑盒 + 冒烟样本 fixtures 覆盖传输矩阵与列表解析;CI 恒离线
