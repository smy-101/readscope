# 06: M3 验收收口

**What to build:** 对齐 M2 07 号先例:以 PLAN 验收标准**"无账号完整读帖跟评"**逐条验收全里程碑,收口文档、键位覆盖与发布面。

**Blocked by:** 01, 02, 03, 04, 05

**Status:** ready-for-human

- [x] 验收清单全过:Front / New / Ask / Show 可刷可读;开帖读文、跟评三层通;离线重启列表即现、已开帖子评论可翻
- [x] ● 未读点 / Ctrl+R / F4 / Ctrl+A / F3 / Ctrl+F 对 HN 全部生效;收藏的帖子从源里消失后仍可读
- [x] F1 帮助完整覆盖 M3 新键位;zh-CN / en 键树一致性测试绿
- [x] README 双语 HN 章节与 PLAN ADR #13、GLOSSARY 术语对齐;`go vet` + `go test ./...`(含 -race)全绿
- [x] 真二进制冒烟:真机 Algolia 拉取一轮(网络受限时记录替代验证方式)

自动化证据、真网三轮冒烟记录与(可选的)用户复核清单见 [../acceptance.md](../acceptance.md)。本票新增钉子:`TestHNReadStateKeysWorkOnHNRows`、`TestHNFavoriteSurvivesFeedEviction`,F1 帮助测试补 `← →` 楼层导航断言;README 双语补三层阅读结构(对齐 ADR #13 的楼层制+两层扁平化+@parent)并更新里程碑状态行。
