# 04: 真网 smoke + 收尾

**What to build:** 用户侧人工验收清单(参照 `.scratch/m4-zhihu/smoke.md` 同款式):真实网络下对照小黑盒 App/网页,核验推荐流与圈流内容一致、帖子两层详情与楼中楼读全、离线可读、风控提示友好、Ctrl+O 跳转正确;以真实转发帖样本校验其渲染(异常则确认降级为标题卡的兜底形态);i18n 完整性终检与 doctor 行收口。CI 保持恒离线,本票是唯一触网验收。

**Blocked by:** 02、03。

**Status:** ready-for-human (agent 侧已落地 2026-11-14,待真网 smoke)

- [ ] 推荐流首屏与 App 首页肉眼同源,无广告卡混入
- [ ] 自选 2–3 个圈,圈流条目与网页版圈页一致,翻页不重复
- [ ] 挑 5 个帖子(纯文本/多图/视频/文章/转发各一)走两层详情,全部可读且降级形态符合 spec
- [ ] 挑一个 100+ 楼高热帖,Ctrl+N 读到尽头,楼中楼补全无缺漏
- [ ] 断网重启:列表/正文/评论全部可读
- [ ] `readscope heybox add <新圈>` 真网加圈成功
- [x] doctor 小黑盒行输出圈数与最近拉取结果(票 02 已落地;TestHeyboxLineReportsCirclesAndLastFetch 双语钉住「N 圈 · N 条快照 · 最近拉取 X」/「N 圈 · 从未拉取」,坏圈表失败点名;真网形态在清单 §0/§9 复看)
- [ ] 语言切换 zh-CN/en,新增文案无缺漏(agent 侧已双层钉住:键树 parity + 本票新增动词 parity 测试;真网切换走查在清单 §9)
- [ ] 转发帖真样本核验结论回填本票 Comments(正常渲染或确认降级兜底)

## Comments

**Agent 侧收口(2026-11-14)**:①用户侧验收清单落地 `.scratch/m5-heybox/smoke-acceptance.md`(m4 smoke.md 同款式,十节:准备/加圈 CLI/推荐流/圈流/五类帖两层详情/100+ 楼读到尽头/风控机会项/离线/收藏快照/doctor+双语/收尾;每步铆在实现事实上——键位取自 helpBindings,页容量取自冒烟矩阵 P14=10/圈流=20,doctor 文案取自 doctor.* 键,转发帖核验项承接 03 号票遗留的「真样本校验」)。②i18n 终检:既有键树 parity 之上新增 TestParityFormatVerbsMatch——格式动词序列 zh↔en 逐键一致(zh `%d` 配 en `%s` 这类运行时 `%!d` 炸点提前到 CI;变异测试验证:临时改 en `heybox.up` 为 %s 立红,复原全绿),静态扫描无绕过语言包的硬编码文案(仅注释与 fixture),动态键(`source.*`/`help.keys.*`/`heybox.recommend` 经 recommendLabelKey)全部在包。③doctor 小黑盒行经查已在票 02 完整落地(圈数 + LastFetched + FeedCounts 快照数,双语 + 坏圈表失败三测试),本票零代码改动、验讫。④`go vet ./...` + `go test -race ./...` 全绿(与 CI 三步等价,除远端 push),二进制重编(25MB,gitignore 内)。剩余真网 smoke 待用户走查(清单 `.scratch/m5-heybox/smoke-acceptance.md`,建议顺序:准备→加圈→推荐流/圈流→五类帖→100+ 楼→断网→doctor/双语),偏差与转发帖结论回填本票后 M5 收官。
