# 02: 圈流 + 加圈 CLI

**What to build:** `readscope heybox add <关键词>` 一条命令完成加圈:免签圈名搜索 → 终端列出候选(名称/帖子量等元信息)→ 编号选择 → 写回 sources.toml → 源重建后侧栏多出该圈的 feed。每个圈一条 feed(RSS 订阅同构),刷圈流、Ctrl+N offset 翻页、已读/缓存与全源同语义;TOML 手改圈列表永远兜底(改完重启或重载生效);doctor 的小黑盒行报圈数与最近拉取结果。

**Blocked by:** 01(传输层与源骨架)。

**Status:** ready-for-agent

- [ ] `readscope heybox add 黑神话` 列出候选圈,选号后 TOML 落盘、侧栏出现该圈 feed
- [ ] 圈流 F5 拉取成功(20 帖/页量级),条目与推荐流同为帖子卡,翻页/已读/离线语义一致
- [ ] 多圈并存:每圈独立 feed、独立页深与缓存,互不串页
- [ ] 搜索无结果/网络失败有友好提示,不写 TOML
- [ ] TOML 手改圈列表(增/删/排序)后源按声明序呈现
- [ ] doctor 报圈数与最近拉取结果
- [ ] 加圈流程与圈列表解析各有测试(fake 搜索服务端 + TOML round-trip)
