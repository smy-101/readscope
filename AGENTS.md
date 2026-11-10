# readscope

终端里的统一阅读器:RSS/HN 信息流 + 本地 EPUB 书库。Go + Bubble Tea 单二进制。
总体方案见 `docs/PLAN.md`(含已定决策 ADR 一览与里程碑)。

## Agent skills

### Issue tracker

Issues 以本地 markdown 文件形式存放在 `.scratch/<feature-slug>/` 下。见 `docs/agents/issue-tracker.md`。

### Triage labels

使用默认五角色标签(needs-triage / needs-info / ready-for-agent / ready-for-human / wontfix)。见 `docs/agents/triage-labels.md`。

### Domain docs

单上下文布局(GLOSSARY.md + docs/adr/ 于仓库根)。见 `docs/agents/domain.md`。
