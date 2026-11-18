# 编码规范

双轴评审 Standards 轴的执法依据,实现时同样适用。每条规则都从历次评审挣来,附出处票(`.scratch/` 下)。

## 行宽不变量

详情层一切行组装(body / floorLines / replyLines)以 `detailWidth()` 为消费宽。新写行组装的测试,一行 `assertLinesFit(t, lines, m.detailWidth())` 钉住(助手在 `internal/ui/widthguard_test.go`);先折行后加前缀的布局,前缀宽度从布局宽里扣。

出处:comment-style/01 P1——深层行按全宽布局再 +2 缩进,占宽溢出 1 列,被 lipgloss 定宽再折一字破坏列对齐。

## i18n 断言组合成品

断言拼接后的成品串,zh-CN 与 en 都断言。键树 parity 与格式动词 parity(既有机械层)测不出拼接错。

出处:m5-heybox/02 P1——zh 尾缀「前」× `time.minutes`「%d 分钟前」拼出「前 前」,en-only 断言 + 键存在 parity 双双漏网,组合断言才红。

## 两处容忍,第三处提炼

近重复(同形小函数)与按源分叉(`d.post ? heybox.* : zhihu.*` 一类)在两处规模保留;出现第三处消费者或分叉点,即提炼。已挂账的容忍对:heyboxMeta/hnMeta、doctor relTime/ui relTime、detail 的 post/article 分叉。

出处:m5-heybox/01–03 评审保留项(两处未到提炼线,先例可辩护)。

## 注释声明可验

注释里写「测试钉住 X」「匹配 Y」时,X/Y 必须真实存在且指得出;声明与实现漂移时,删声明或补实践。

出处:m5-heybox/01(WithClient 注释谎称有测试用例)、04 评审(verbRe 字符类自称匹配 `%-3.2f` 实则零匹配)。
