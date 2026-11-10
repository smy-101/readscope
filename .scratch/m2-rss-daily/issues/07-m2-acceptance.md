# 07: M2 收口验收

**What to build:** M2 全部功能合拢后的收口:doctor 增加 feed 可达性检查(每个订阅试连并报告),F1 帮助屏补全本里程碑全部键位,README 中英文补 RSS 使用说明(refresh/OPML/键位),最后按 PLAN 验收标准做真机验收:日常订阅可刷可读,断网重启列表即现、正文可读。

**Blocked by:** 03(详情层 — 全文阅读), 04(已读/收藏状态机), 05(ETag 条件请求), 06(OPML 导入/导出)

**Status:** ready-for-agent

- [ ] doctor:逐 feed 可达性检查,输出 明确的 成功/慢/失败
- [ ] F1 帮助屏覆盖 M2 全部键位(Enter/Esc/F4/F5/F3/Ctrl+R/Ctrl+A/Ctrl+F/Ctrl+O/Ctrl+Y)
- [ ] README / README.zh-CN 补 RSS 用法(订阅、刷新、OPML、键位表)
- [ ] CI 绿,`go test ./...` 全过
- [ ] 真机验收清单执行并记录:真实订阅连续刷读、断网重启验证、已读/收藏跨重启
