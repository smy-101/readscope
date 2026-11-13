# 02: 圈流 + 加圈 CLI

**What to build:** `readscope heybox add <关键词>` 一条命令完成加圈:免签圈名搜索 → 终端列出候选(名称/帖子量等元信息)→ 编号选择 → 写回 sources.toml → 源重建后侧栏多出该圈的 feed。每个圈一条 feed(RSS 订阅同构),刷圈流、Ctrl+N offset 翻页、已读/缓存与全源同语义;TOML 手改圈列表永远兜底(改完重启或重载生效);doctor 的小黑盒行报圈数与最近拉取结果。

**Blocked by:** 01(传输层与源骨架)。

**Status:** done (2026-11-13,agent 侧;真网 smoke 与手感查走 04 号票)

- [x] `readscope heybox add 黑神话` 列出候选圈,选号后 TOML 落盘、侧栏出现该圈 feed
- [x] 圈流 F5 拉取成功(20 帖/页量级),条目与推荐流同为帖子卡,翻页/已读/离线语义一致
- [x] 多圈并存:每圈独立 feed、独立页深与缓存,互不串页
- [x] 搜索无结果/网络失败有友好提示,不写 TOML
- [x] TOML 手改圈列表(增/删/排序)后源按声明序呈现
- [x] doctor 报圈数与最近拉取结果
- [x] 加圈流程与圈列表解析各有测试(fake 搜索服务端 + TOML round-trip)

## Comments

- **2026-11-13 实现完成**(CI 恒离线;真网验证归 04 票)。落点:
  - **圈表**:`sources.toml` 增 `[[heybox.circles]]` 数组表(name + topic_id)——数组而非 map 保声明序 = 侧栏序;`Sources.Heybox` 为 nil 指针,无圈时 marshal 不落 `[heybox]` 节(默认 Save 不长脏段)。
  - **圈流**:feed key `circle/<topic_id>`(id 稳定于改名);`FetchPage` 按 key 分派,圈流走 `/bbs/app/topic/feeds`,参数逐字镜像冒烟 S8/P15(topic_id/offset/limit=10/lastval=""/dw=720;服务端自 serve ~20 卡)。卡片管线与推荐流共享(linkCard 同形),`cardItem` 参数化 feed key。真实 08 号样本裁剪转正 `testdata/topic-page.json`(视频帖/无题帖/cube 标记)。
  - **re-homing**:同一帖在推荐流与圈流出现时单行搬移、已读随行——HN Front/New 既有语义,refresh tracer 钉住防"修成"重复行。
  - **免签搜索**:传输层拆出 `GetUnsigned`(只带端点自身参数,无签名无公参——冒烟 07 的实测形状;同浏览器头/风控哨兵/页隙)。搜索 limit 取 10(冒烟探针为 5;页大小非协议形状)。`SearchTopics` 解析 topics[](show_desc 热度串原样透传),坏条目(无 id/无名)静默跳过。
  - **加圈 CLI**:`readscope heybox add <关键词…>`(多词 join);构造 Source 先验证既有圈表(坏手改在任何拨号前拒绝)→ 搜索 → 编号列表(名称 + 热度串)→ stdin 选号(坏序号重提示,q/EOF 取消)→ `Subscribe` 追加去重 → Save。退出码:0 = 已加/已订/取消,1 = 无结果/搜索失败/写失败,2 = 用法。取消/无结果/失败一律不写 TOML。
  - **校验门**:`ValidateCircles`(name 必填、topic_id > 0、重复 topic_id 拒绝)单点持有,F5/Ctrl+N/CLI/doctor 共用。
  - **doctor**:第 8 项检查「小黑盒源」= 圈数(经校验)+ 快照条数 + 最近拉取相对时间(store 新增 `LastFetched`:ORDER BY fetched_at DESC LIMIT 1,聚合并丢 TIMESTAMP decltype 会让驱动回 string——注释记录了这一坑);恒离线不拨号,「从未拉取」是装机初态的诚实注记而非失败;坏圈表失败并点名条目。
  - **i18n**:heybox.cli_* 与 doctor.heybox* 双语齐备,parity 测试过。
- 待 04 票:真网加圈一次冒烟(搜索/圈流/doctor 行)、20 帖页量级与热度串文案实勘。

**评审(2026-11-13,Standards+Spec 双轴,并行子代理,68552f1..fe8ac6a)**:Spec 轴 **P1 已修**——zh `doctor.heybox_state` 自带尾部「前」与 `time.minutes` 的「%d 分钟前」组合出「最近拉取 5 分钟前 前」;原测试只钉 en、parity 只查键存在,故漏网。修法:去掉尾部前缀,doctor 测试改为 en/zh 双语**组合**断言(含「前 前」garble 守卫)——组合才测得出这类拼接错,键存在测不出。Spec P2 已修:加圈测试补上仪式最后一环链路断言(LoadSources→heybox.New→Feeds() 含 circle/568547 且标签对)。P2 保留:doctor「最近拉取结果」实现为「最近内容落地时间」(失败不动),代码注释已论证;若要成败语义,feed_fetch_state 是自然来源,留待有真实需求再说。Standards 轴(全 P2 判断项,无硬违规):已修——`Topic.Hot` 删字段(解析无人读,Speculative)、Get/GetUnsigned 共享 `pace` 前奏(去重)、还原 zh 包两个既有串的顺手标点变更(risk_later/cookie_perms,diff 聚焦);保留——doctor relTime 与 ui Model.relTime 双胞胎(doctor 不能 import ui,注释自认;若再添第三处消费者则提炼 Catalog.RelTime)、cmd 测试与包内测试的搜索夹具重复(跨包可辩护)、CLI 的 `.(*heybox.Source)` 断言式门(注释论证,与 zhihu 包级门分歧已知)。
