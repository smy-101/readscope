# 01: 数据面 tracer — `zhihu probe` + cookie 注入 + 止损判定

**What to build:** M4 的第一条 tracer bullet,先证明数据面再写一行 UI。`readscope zhihu probe` 子命令:读 cookie 文件(参数可传路径),按 spec 数据面契约逐端点请求(热榜/日报列表/日报正文/关注流/问题元信息/回答 feeds/评论 root),逐端点打印 HTTP 状态 + 关键字段解析结果(qid 精度、问题卡过滤、楼层头三元组、评论缺口),原始响应落盘 `.scratch/m4-zhihu/samples/`。头矩阵自检:x-zse-93 带/不带在 v4/questions 与 feeds 两个端点的差异、缺失 cookie 时各端点行为。产出止损结论(Go 直连全绿 / 被拦与被拦端点清单),写回本票 `## Comments` 并同步 spec 风险表。构建与离线测试不需要 cookie;真跑需要用户小号 cookie(先于 07 号票的用户步骤)。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] `readscope zhihu probe` 逐端点探测:HTTP 状态 + 结构解析摘要,原始样本落盘 `.scratch/m4-zhihu/samples/`(目录已 gitignore,关注流含个人数据)
- [x] cookie 文件读取与注入(缺失时免登录端点照跑,登录端点 SKIPPED-NO-COOKIE)→ 多行粘贴规范化拼接,z_c0/d_c0 判活,cookie 值永不进输出
- [x] 头矩阵自检 → 两负向期望均 MISMATCH:门禁已放宽(见 Comments)
- [x] 大整数精度:热榜 qid 从 URL 提取,真网 qid=2103623629674132899 实锤 >2^53
- [x] json.Number 解析路径与从-URL-提取路径的单测(fixture,离线)→ 另有 float64 丢精度对照测试钉住 rationale
- [x] 止损结论记录:全端点 GREEN(见 Comments),spec 风险表已同步

## Comments

**2026-11-12 判定(真网,用户小号 cookie):Go 直连全绿,免签路线成立,止损阶梯未动用。**

| 端点 | 判定 | 现场证据 |
|---|---|---|
| 热榜 api 门 | GREEN | 30 行;qid 从 target.url 提取,>2^53;detail_text 热度列活着 |
| 热榜 www 门 | GREEN(需会话) | 带 cookie 200;匿名 401 code=101 → 归登录族;作 api 门的备份门保留在 probe 里 |
| 日报列表/正文 | GREEN | 列表 4 条 hint 元信息在场;正文 7KB HTML,离线快照成立 |
| 关注流 | GREEN | 18 卡:5 answer 卡保留、13 丢弃(空类型 6/pin 6/article 1);paging.next 游标在场 |
| 问题元信息 | GREEN | 带 x-zse-93 200;title/author/answer_count 解析(样本问题 detail=0B) |
| 回答 feeds | GREEN | 不带 x-zse-93 200;5 答;楼层头三元组 author·赞·N 评论解析;游标在场 |
| 评论 root | GREEN | 10 根;首根 like=115、child_gap=0(缺口公式由 fixture 锁定) |

头矩阵自检(两项均 MISMATCH,门禁放宽):question-meta 不带 x-zse-93 → 200(FeedScope 2026-11 矩阵为 403 code=10003);feeds 带 x-zse-93 → 200(旧矩阵 403)。处置:按旧矩阵形态带/不带仍全 200,维持现状;放宽只提高鲁棒性,x-zse-93 常量单点保留。

风控观察:纯 Go http.Client + 桌面 Chrome UA + x-requested-with + Accept-Language,无头序工程,全端点 200——「非浏览器指纹被拦」头号风险证伪(单次判定;后续若间歇 403 再上阶梯①)。

实现落点:`internal/source/zhihu/`(zhihu.go 常量/ids.go/cookie.go/probe.go)+ `cmd/readscope/zhihu.go`;离线测试三件套(URL 提取与精度对照、cookie 解析、假知乎全端点+头矩阵+链式 id+样本落盘)。`READSCOPE_ZHIHU_BASE` 可整体换 host 根,`-qid/-story/-aid` 可跳链重跑深端点。
