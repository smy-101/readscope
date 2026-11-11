# M2 真机验收记录

对应 ticket 07。自动化部分已在本会话完成并留证;真机交互部分由用户执行,结果记在下方。

## 自动化验证(2025-11-11,本会话)

| 项 | 证据 |
|----|------|
| CI 等价三步 | `go vet ./...`、`go test -race ./...`、`go build ./...` 本地全绿(与 ci.yml 步骤一致);远端 CI 待 push 后绿 |
| 真实订阅连续刷读(CLI 面) | 阮一峰 atom:首次 `3 new / 3 total`,二次 `unchanged / 3 total`(ETag 304 链路生效),退出码均 0 |
| doctor 逐 feed 检查 | 真源 `✓ 阮一峰周刊 (991ms)`;回环 404 → `✗ Bad Path: HTTP 404`、exit 1;慢路径(>3s)注入探针测试覆盖;无订阅 → 提示行、exit 0 |
| F1 帮助屏 | `TestHelpOverlayListsM2Keys` 钉住全部 M2 键位(Enter/Esc/F3/F4/F5/Ctrl+R/A/F/O/Y)及描述 |
| i18n 完整性 | 新增 doctor.* 键 zh-CN/en 双语,键树一致性测试过 |
| 缓存先行/离线渲染 | `internal/ui` 既有测试覆盖(cache-first 列表、详情全文);交互面留真机验证 |

## 真机清单(用户执行)

环境:真实终端(WSL 或裸 Linux),日常用的订阅 2–3 个写进 sources.toml。

- [ ] **连续刷读**:启动 `readscope`,Enter 读全文、Esc 返回、F5 刷新看状态栏计数;`readscope rss refresh` 再刷一次,应大幅快于首次(304)
- [ ] **断网重启**:断网状态下重启 `readscope` —— 列表即现(非空、非加载态),Enter 全文可读
- [ ] **已读/收藏跨重启**:Ctrl+R 切已读、Ctrl+F 收藏、F4 只看未读、F3 收藏列表、Ctrl+A 全部已读(确认框);重启后以上状态全部保留
- [ ] **doctor 真机**:`readscope doctor` 全绿;如某订阅 >3s 显示 `!` 慢(警告)不影响退出码
- [ ] (可选)从现有阅读器导出 OPML,`readscope rss import` 后列表完整;`export` 回导能在原阅读器读回

执行日期与备注:

> (在此记录)
