# 07: M4 验收 — 真网 smoke + 收口

**What to build:** M4 收口。真网 smoke(用户侧,主号或验证后小号 cookie):热榜/关注/日报三 feed 刷新、开 5 个问题走完三层、Ctrl+U 换号、拔 cookie 验证降级;agent 侧完成 README(知乎源配置/cookie 获取步骤/风险提示)、F1 帮助收口(Ctrl+U)、i18n 双语键树校验、验收清单逐项勾选并把真实网络行为的偏差记录进本票 Comments。

**Blocked by:** 03, 05, 06

**Status:** ready-for-human (agent 侧已落地 2026-12-08,待真网 smoke)

- [ ] 真网 smoke:三 feed 刷新正常,条目数与网页侧肉眼一致
- [ ] 真网 smoke:5 个问题走完 问题层→回答层→评论层,含一个高热度长回答(评论补全路径)
- [ ] 真网 smoke:Ctrl+U 粘贴新 cookie 生效;删 cookie 文件后热榜/日报存活、关注 feed 引导登录
- [x] README:知乎源开启、cookie 获取(DevTools 步骤)、失效重登、账号风险提示(双语,含 Ctrl+U 键位行)
- [x] F1 帮助含 Ctrl+U(login_test 已断言);i18n zh-CN/en 键树一致,新增键全部双语
- [ ] 验收清单逐项勾选;真网偏差(字段/行为与 fixture 不符处)记录 Comments 并回补 fixture

## Comments

**Agent 侧收口(2026-12-08)**:README.md / README.zh-CN.md 双语补齐知乎整节(三 feed 概览、问题三层、cookie 登录四步、z_c0 拒收、zhihu.cookies 0600 与 doctor 报告、保存即用/失效重登、删文件降级语义、账号风险提示含小号建议与 probe 排障),键位表补 Ctrl+U 行;「v0 不做」清单移除知乎、里程碑措辞改为 M4=知乎/M5=EPUB。F1 帮助与 i18n 经查已在 04/06 号票落地到位(helpBindings 含 zhihu_login,login_test 断言 F1 overlay 含 Ctrl+U 行;parity 测试过,en/zh 键树完全一致,zhihu 键全部双语)——本票零代码改动,纯文档。剩余三项真网 smoke 待用户侧走查(清单见 `.scratch/m4-zhihu/smoke.md`,建议顺序:三 feed 刷新→5 问题三层含长回答评论补全→Ctrl+U 换号→删 cookie 降级),偏差回补 fixture 后勾最后一项。

**评审(2026-12-08,Standards+Spec 双轴,并行子代理)**:两轴共同 P1「英文版 sources.toml 片段/升级注未提 zhihu」经对树核实为**误报**——README.md:51/65 在先前提交已含 `"zhihu"` 与双源升级注,中文版才是落后待补的一方(评审仅见 diff,无文件访问,推断失效)。真修四项已落地:①英文 gloss「(关注)」→「(关注流)」(GLOSSARY 正名);②三层描述补回答层语义(Enter 读单答全文、←→ 移动,双语);③键位表尾行空格收到末行;④本票 Status 从非五角色的 claimed 改回 ready-for-human(剩余工作恰是用户侧 smoke)。Spec 侧「zh 括号不闭合」同为误报(见下在,评审误读)。无 scope creep;双轴 merge verdict 均 OK with notes。

**真网偏差 #1(2026-12-08 smoke,已修)**:用户报「看不到评论的评论」。根因:①首页游标——`FetchAnswerComments` 把首页拼成 `offset=0`,真网对字面数字 offset 一律回 HTTP 200 空页(偠尸页,next 仍指 offset=0,5/5 回答复现;`offset=` 空才有数据),放行到 UI 即评论层零条;②翻页游标——真网 `paging.next` 的 offset 是复合串(实测形如 `387770585_11616175634_0`、`5_11616058687_0`),`offsetFromURL`(int)装不下,Ctrl+N 必卡死。修复:接缝改游标制(与 FetchFloorsPage 同约定),`FetchAnswerComments(cursor string)`、首页不带 offset 参数、翻页 verbatim 跟走 paging.next、`""`=到底;UI `commentsNext` string 化;删 offsetFromURL。fixture 回补:fake 建模偠尸页 tripwire(数字 offset→200 空页)+ 复合游标页(`path?query` 寻址),契约测试 `TestFetchAnswerCommentsRealCursorContract` 红→绿。child_comment 补全路径实测本就正确(空 offset 首页 + verbatim next,23 条补全)。附带修复:`readscope zhihu probe` 双重切片致空参 panic(zhihu.go:46)。

**风控观察**:诊断期间连发 probe+curl+harness 后,comment_v5 → v4 全家对携带 cookie 的请求间歇 403(裸 curl 同 cookie 前一刻仍 200;匿名热榜不受影响),符合 spec 风险表「间歇 403」预期,休息后自愈;这也实证了 doctor/probe 作为排障出口的必要性。
