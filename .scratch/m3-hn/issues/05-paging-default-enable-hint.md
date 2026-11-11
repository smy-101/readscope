# 05: 收尾机制 — 列表翻页 + 默认启用 + 空白提示

**What to build:** 三件收尾机制:① HN 列表 Ctrl+N 拉 Algolia 下一页追加进缓存(与 F5 刷首页互补);② 新装默认启用 `["rss","hn"]` 零配置可用,存量 sources.toml 不自动改写(README 记手动加法);③ Tab 切到未注册源(书架)时状态栏显示"后续里程碑提供",消除谜之空白(本次 M3 的起因)。

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Ctrl+N 追加下一页:不产生重复行、已读状态保持;状态栏反馈加载/失败/到底
- [ ] F5 仍刷第一页;Ctrl+N 追加与 F5 刷新语义互不破坏 → review 转入(spec 决策:F5 = 重拉**当前 feed** 第一页):当前 F5 走 refresh.Source 刷全源四 feed(源无关机制,可见需求成立,代价 3 个多余 GET);本票做页感知拉取时一并改作 feed 级刷新
- [x] 全新环境首启默认 enabled = `["rss","hn"]`,零配置可见 HN(01 已交付,commit 5784259)
- [x] 已存在的 sources.toml 不被自动改写;README(zh-CN / en)记录手动启用与 HN 用法(01 已交付)
- [x] 切到未注册源(书架)状态栏显示"后续里程碑提供";已注册源不受影响(01 已交付,TestTabToUnimplementedSourceHintsLater)
