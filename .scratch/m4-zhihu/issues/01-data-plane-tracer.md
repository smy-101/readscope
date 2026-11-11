# 01: 数据面 tracer — `zhihu probe` + cookie 注入 + 止损判定

**What to build:** M4 的第一条 tracer bullet,先证明数据面再写一行 UI。`readscope zhihu probe` 子命令:读 cookie 文件(参数可传路径),按 spec 数据面契约逐端点请求(热榜/日报列表/日报正文/关注流/问题元信息/回答 feeds/评论 root),逐端点打印 HTTP 状态 + 关键字段解析结果(qid 精度、问题卡过滤、楼层头三元组、评论缺口),原始响应落盘 `.scratch/m4-zhihu/samples/`。头矩阵自检:x-zse-93 带/不带在 v4/questions 与 feeds 两个端点的差异、缺失 cookie 时各端点行为。产出止损结论(Go 直连全绿 / 被拦与被拦端点清单),写回本票 `## Comments` 并同步 spec 风险表。构建与离线测试不需要 cookie;真跑需要用户小号 cookie(先于 07 号票的用户步骤)。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] `readscope zhihu probe` 逐端点探测:HTTP 状态 + 结构解析摘要,原始样本落盘 `.scratch/m4-zhihu/samples/`
- [ ] cookie 文件读取与注入(文件缺失时免登录端点照跑,登录端点明确标注 SKIPPED-NO-COOKIE)
- [ ] 头矩阵自检:v4/questions 缺 x-zse-93 = 10003;feeds 带 x-zse-93 = 403;两者带正确头 = 200
- [ ] 大整数精度:热榜 qid 从 URL 提取,样本含 2^53 以上 id
- [ ] json.Number 解析路径与从-URL-提取路径的单测(fixture,离线)
- [ ] 止损结论记录:每端点判定(GREEN / BLOCKED),BLOCKED 时标注建议阶梯档位
