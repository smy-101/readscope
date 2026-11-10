// code.go: syntax highlighting via chroma, mapped into the logical line
// model — token categories become SpanKw/SpanStr/SpanNum/SpanCom bits so
// golden tests stay deterministic (no ANSI) and themes color later.
package render

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// HighlightLines tokenises src (by language name when known) and returns
// one span slice per source line, every span carrying SpanCode plus a
// category bit. Unknown languages degrade to plain SpanCode lines.
func HighlightLines(lang, src string) [][]Span {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		return plainCodeLines(src)
	}
	iter, err := lexer.Tokenise(nil, src)
	if err != nil {
		return plainCodeLines(src)
	}

	var lines [][]Span
	var cur []Span
	appendSeg := func(text string, style SpanStyle) {
		cur = append(cur, Span{Text: text, Style: SpanCode | style})
	}
	for _, tok := range iter.Tokens() {
		style := tokenStyle(tok.Type)
		// A token may span multiple lines (block comments, strings).
		for i, seg := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				lines = append(lines, cur)
				cur = nil
			}
			if seg != "" {
				appendSeg(seg, style)
			}
		}
	}
	if cur != nil {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = append(lines, []Span{{Text: "", Style: SpanCode}})
	}
	return lines
}

func tokenStyle(t chroma.TokenType) SpanStyle {
	switch {
	case t.InCategory(chroma.Comment):
		return SpanCom
	case t.InCategory(chroma.Keyword):
		return SpanKw
	case t.InCategory(chroma.LiteralString):
		return SpanStr
	case t.InCategory(chroma.LiteralNumber):
		return SpanNum
	default:
		return 0
	}
}

func plainCodeLines(src string) [][]Span {
	var lines [][]Span
	for _, l := range strings.Split(src, "\n") {
		lines = append(lines, []Span{{Text: l, Style: SpanCode}})
	}
	return lines
}
