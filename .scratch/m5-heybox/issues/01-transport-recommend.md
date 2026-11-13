# 01: 传输层 + 推荐流贯通

**What to build:** 在 sources.toml 启用 `heybox` 后,源栏出现小黑盒,F5 刷出匿名推荐流并在终端可刷可读:传输层单点持有签名(变体 A,参考向量恒测)、web 公参(999.0.3 精简集)、安装时生成并持久复用的 device_id、风控哨兵(show_captcha/429/版本门槛 → 「稍后再试」,当次失败不清缓存)与页间延时;列表层产出帖子卡(hex link_id 主键、赞/评论热度快照、share_url 外链),广告卡(无 linkid)静默丢弃;Ctrl+N offset 翻页、已读/未读点、缓存先行离线可读、双语 UI 键齐备。

**Blocked by:** 无(可立即开始)。

**Status:** done (2026-11-13,agent 侧;双轴评审已过——P1 页间隙账本提为包级 dialLedger 跨 transport 生效,真网文案校准与手感查走归 04 号票)

- [x] 启用 heybox 后源栏出现小黑盒,推荐流 F5 拉取成功,条目显示标题/摘要/作者/赞数/评论数(摘要落 Body 快照,标题列照全源惯例;02 票前无 doctor 行)
- [x] 签名器对 4 组参考向量(原版 JS 固定输入生成)恒测通过;签名只依赖 canonical path(另有 B 变体随版本开关一并入册,同样恒测)
- [x] device_id 安装时随机生成 32hex 持久化,后续请求复用(重启不变;坏文件拒绝重建,不静默重铸)
- [x] 广告卡不出现在列表;帖子卡 id 形如 `hb:link/<hexid>`,Ctrl+O 打开 share_url
- [x] Ctrl+N 翻页命中前进、到底提示,重启后续翻不重拉(refresh tracer 钉住拨号序列)
- [x] show_captcha/429/版本过低 → 状态栏「稍后再试」级提示,不自动重试,已缓存内容不受影响(版本门槛的 msg 匹配为预留宽匹配,真网文案归 04 票校准)
- [x] 断网重启后列表即现、条目可读;页间延时可观察(无 429)(传输层 1.5s 预间隙,账本跨 transport 按基址共享——F5/Ctrl+N 各自新建 source 也能起效,可观察测试入 transport_test)
- [x] 新增 UI 文案 zh-CN/en 双语齐备,语言包完整性测试过
- [x] httptest 假小黑盒 + 冒烟样本 fixtures 覆盖传输矩阵与列表解析;CI 恒离线(P14 裁剪样本转正 testdata/recommend-page.json)

## Comments

**评审(2026-11-13,Standards+Spec 双轴,并行子代理,044a24f..fab11c5)**:Spec 轴 P1 已修——页间延时原是死代码(运行时每次 F5/Ctrl+N 新建 source/transport,per-transport lastReq 永不触发,README 礼貌限速承诺为假):账本提为包级 dialLedger 按基址共享、跨 transport 生效(仅进程内,重启即空窗);TestTransportGapSpansTransports 钦住;READSCOPE_HEYBOX_MINGAP(ms)可缩放间隙,测试归零、真网 smoke 可调。P2 保留:heybox 默认开启随 zhihu 滚动先例(F5 是用户意图,无后台拨号);cookie 钩子 spec 预留未建,transport 头注释标明将来挂点。Standards 轴:WithClient 删除(零调用者,注释谎称有测试用例);两处小写十六进制 rune 循环合并为 isLowerHex;heyboxMeta/hnMeta 近重复按全源 meta 路由的房屋风格保留;sign.go 不透明命名(zA/jA/oA)保持原实现拼写,端口可逐行对照、参考向量钦住。
