# 04: cookie 管理 + 登录仪式 + 失效语义

**What to build:** 登录态基础设施。cookie 文件 `~/.config/readscope/zhihu.cookies`(0600,原样粘贴的 Cookie 请求头文本;解析出 z_c0/d_c0 判活,其余键值原样透传)。`readscope zhihu login` 子命令:提示用户从浏览器 DevTools 复制 Cookie 头,粘贴→校验(含 z_c0?)→写入;TUI 内 Ctrl+U 弹同一输入 dialog,保存即用免重启。会话失效语义:详情族(v4/comment_v5)401/403 → 状态栏"登录态失效,Ctrl+U 重新登录",不清 cookie、热榜/日报不受影响;列表族(v3)403 视为纯风控,原样报错。doctor 报告 cookie 文件存在/权限/z_c0 有无。所有带 cookie 的请求统一走 zhihu 共享 transport(UA/x-requested-with/Referer/x-zse-93 矩阵单点维护)。

**Blocked by:** 02

**Status:** done (2026-11-29)

- [x] `readscope zhihu login`:粘贴→校验含 z_c0→0600 落盘;Ctrl+U 同款 dialog,保存即用(SaveCookie 单缝:temp+rename 原子 0600,拒收不落盘;UI 拒收后清缓冲留窗口;ctrl+Cli 冒烟:有效/垃圾/空三路)
- [x] 共享 transport:公共头 + 按端点的 Referer/x-zse-93 矩阵单点维护(transport.go 的 Endpoint 表是唯一矩阵源,probe.go 与 source.go 的两个 get 块已删;错版本腿入测试:TestTransportWrongVersionLandsAsSessionExpired —— 客户端无法区分错版本与失效,统一读作 ErrSessionExpired,10003 的辨别归 probe 报告)
- [x] v4 详情 401/403 → 登录引导提示(不清 cookie);v3 403 → 原样报错;两分支 httptest 各有 fixture(TestTransportDetailFamilySessionExpired 401/403 两腿 + TestTransportListFamily403StaysRaw;UI 侧 TestRefreshStatusMapsSessionErrors 把三类错误分别导向 login_expired / login_required / 原文)
- [x] cookie 文件缺失时:关注 feed 显示登录引导(状态栏),热榜/日报正常(机制已落地:Gated 预检 → ErrLoginRequired → UI 引导;TestTransportGatedPreflightRefusesWithoutCookie 断言零拨号、热榜/日报照常。关注 feed 边栏行的呈现随 05 号票落地)
- [x] doctor:cookie 文件存在/权限/z_c0 有无,输出到自检报告(三态:未配置=✓带匿名注释 / 健康=✓键数普查 / 存在但坏=✗ 命名修法;TestZhihuCookieStates)
- [x] cookie 解析单测(整段请求头文本→键值;垃圾输入不崩、明确报错)(TestParseCookieGarbageNeverPanics + TestSaveCookieRefusesGarbage;顺手修了 ParseCookie 尾随多分号残留 bug)

## Comments

- 2026-11-29 实现完成。设计:Transport{hosts,cookie,client} 按源构造时快照 —— New() 每次 refresh 重建源并重读 cookie 文件(z_c0 判活,缺则匿名),Ctrl+U 保存后 F5 即生效,免重启;无进程级单例、无锁。
- Endpoint 矩阵:8 端点行(Referer/x-zse-93/Cookie/Gated/Family)为唯一事实源;探针矩阵自检腿用结构体克隆翻转(Name/ZSE93),问题详情 Referer+qid 在调用点拼。
- 失效语义实现位置:transport 按 Family 分类(详情族 401/403 → ErrSessionExpired;v3/匿名族 → 原文带状态码),UI 的 zhihuSessionStatus 只做 errors.Is 映射;refreshStatus 顺带修了单 feed F5 失败只显 1-of-1 汇总、丢失原因的问题(现在单失败结果直接展示该错误)。
- Ctrl+U dialog:手滚单行输入(无新依赖,不引 bubbles);掩码渲染圆点(掩码上限 48,滚回缓冲不泄会话);括号粘贴 = 一个大 KeyRunes,同一分支累积;拒收(ErrNoZC0)留窗清缓冲,fs 错误关窗。
- doctor 的 check 结构体加 note 字段(成功态带注释:匿名模式说明/键数普查),未配置=通过 —— 匿名是合法形态不是故障;存在但坏(0644/缺 z_c0/不可读)=失败。
- GLOSSARY 增 登录仪式/失效语义 两词条;F1 帮助加 Ctrl+U;usage 更新 zhihu <probe|login>。
- 遗留给 05:关注 feed 边栏行、无 cookie 时列表层的具体引导呈现(机制与文案键均已就位:zhihu.login_required)。留给 07:README cookie 获取步骤/风险提示。

## Review (2026-11-29)

两轴评审(Standards/Spec)并行子代理,各读全量 diff(2547 行)落盘证据。均判 OK-with-notes,无阻断项。

采纳(已修):
- zhihu.go DesktopUA 注释残行(编辑事故)删除——两轴同抓。
- 矩阵自检腿 Referer 丢 qid(实 bug):noZse/withZse 克隆后未拼 qid,负例相对主行走同时变了 zse93 与 Referer 两个头,403/GATE-MOVED 结论不可归因;现已与主走同 Referer。
- probe outcome() 判定改 errors.Is(原 strings.Contains 撞错误文本);报告行去中英混排。
- app.go 两处"session 引导否则原文"合并为 fetchErrText,顺带删 fetchErrStatus 不可达分支。
- login_test.go 哨兵 var 与无用 import 删除。

记录(不修):
- refreshStatus 单 feed 失败改显错误原文:全源生效的行为变化,票内是失效语义的必要件(否则引导被 1-of-1 汇总吞掉),已披露;多 feed 汇总形态有测试钉住。
- 清单第 4 项为机制落地(ErrLoginRequired→UI 引导链有合成消息测试),关注 feed 真实边栏/列表呈现随 05 落地——转交 05 收口。
- list_test.go import 重排为 gofmt 顺带,保留。

转交:
- 05 号票:关注 feed 上线时,用真实 refresh.Feed 走一遍 无 cookie → 状态栏引导 的端到端(补齐 TestRefreshDoneLoginGuidanceLandsInStatusBar 的合成链)。
