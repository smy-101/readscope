# 07: M4 验收 — 真网 smoke + 收口

**What to build:** M4 收口。真网 smoke(用户侧,主号或验证后小号 cookie):热榜/关注/日报三 feed 刷新、开 5 个问题走完三层、Ctrl+U 换号、拔 cookie 验证降级;agent 侧完成 README(知乎源配置/cookie 获取步骤/风险提示)、F1 帮助收口(Ctrl+U)、i18n 双语键树校验、验收清单逐项勾选并把真实网络行为的偏差记录进本票 Comments。

**Blocked by:** 03, 05, 06

**Status:** ready-for-agent

- [ ] 真网 smoke:三 feed 刷新正常,条目数与网页侧肉眼一致
- [ ] 真网 smoke:5 个问题走完 问题层→回答层→评论层,含一个高热度长回答(评论补全路径)
- [ ] 真网 smoke:Ctrl+U 粘贴新 cookie 生效;删 cookie 文件后热榜/日报存活、关注 feed 引导登录
- [ ] README:知乎源开启、cookie 获取(DevTools 步骤)、失效重登、账号风险提示
- [ ] F1 帮助含 Ctrl+U;i18n zh-CN/en 键树一致,新增键全部双语
- [ ] 验收清单逐项勾选;真网偏差(字段/行为与 fixture 不符处)记录 Comments 并回补 fixture
