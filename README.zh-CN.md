# readscope

> 终端里的统一阅读器 —— RSS 订阅、Hacker News 和本地 EPUB 书库，一个终端全装下。

**语言**: [English](README.md) · 中文

状态：**0.x，开发中。** readscope 是 Go 编写的终端优先阅读器：信息流（RSS/Atom 订阅 + Hacker News）与本地 EPUB 书架共用一套纯键盘、对中文输入法友好的界面。形态仿 [FeedScope](https://github.com/howardli/FeedScope)。

## v0.1 规划特性

- **信息流**：RSS/Atom 订阅，OPML 导入/导出；Hacker News 走公开 Algolia API——免登录、免 key
- **EPUB 书架**：书库目录扫描，`add` 拷入书籍；独立全屏阅读模式，含目录导航、书签、全文搜索
- **状态不丢**：已读标记、收藏（完整快照）、阅读进度存 SQLite；缓存先行渲染，离线可读
- **中文友好的渲染**：RSS 正文与 EPUB 章节共用一条 HTML→终端排版管线，中文折行（禁则处理）、代码高亮
- **输入法安全键位**：全部非打印键（Tab / 方向键 / F 键），中文 IME 激活时照常工作
- **中英双语 UI** + 明暗双主题

v0 不做：登录态抓取源（知乎/X/Reddit）、终端内图像渲染、批注导出、mobi。

## 安装

尚未发布——M0 进行中。源码构建（Go 1.24+）：

```bash
git clone https://github.com/smy-101/readscope.git
cd readscope
go build ./cmd/readscope
```

## 使用

```bash
readscope            # 启动 TUI（默认）
readscope doctor     # 环境自检
```

## 开发

```bash
go build ./...       # 构建
go test ./...        # 测试
go vet ./...
```

设计文档：[docs/PLAN.md](docs/PLAN.md)。

## 许可

[MIT](LICENSE)
