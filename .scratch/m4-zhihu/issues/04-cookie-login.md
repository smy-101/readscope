# 04: cookie 管理 + 登录仪式 + 失效语义

**What to build:** 登录态基础设施。cookie 文件 `~/.config/readscope/zhihu.cookies`(0600,原样粘贴的 Cookie 请求头文本;解析出 z_c0/d_c0 判活,其余键值原样透传)。`readscope zhihu login` 子命令:提示用户从浏览器 DevTools 复制 Cookie 头,粘贴→校验(含 z_c0?)→写入;TUI 内 Ctrl+U 弹同一输入 dialog,保存即用免重启。会话失效语义:详情族(v4/comment_v5)401/403 → 状态栏"登录态失效,Ctrl+U 重新登录",不清 cookie、热榜/日报不受影响;列表族(v3)403 视为纯风控,原样报错。doctor 报告 cookie 文件存在/权限/z_c0 有无。所有带 cookie 的请求统一走 zhihu 共享 transport(UA/x-requested-with/Referer/x-zse-93 矩阵单点维护)。

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] `readscope zhihu login`:粘贴→校验含 z_c0→0600 落盘;Ctrl+U 同款 dialog,保存即用
- [ ] 共享 transport:公共头 + 按端点的 Referer/x-zse-93 矩阵单点维护(01 号票头矩阵结论固化;含 01 评审转交:x-zse-93 **错版本**腿入测试——01 实测门禁已放宽,错版本行为未知,02–06 的失效语义测试补上)
- [ ] v4 详情 401/403 → 登录引导提示(不清 cookie);v3 403 → 原样报错;两分支 httptest 各有 fixture
- [ ] cookie 文件缺失时:关注 feed 边栏/列表显示登录引导(状态栏),热榜/日报正常
- [ ] doctor:cookie 文件存在/权限/z_c0 有无,输出到自检报告
- [ ] cookie 解析单测(整段请求头文本→键值;垃圾输入不崩、明确报错)
