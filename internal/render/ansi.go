// ansi.go: logical lines → ANSI string. The pipeline never emits ANSI;
// this leaf maps SpanStyle bits through a caller-supplied resolver so
// render stays independent of the theme package.
package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StyleFor resolves a span's style bits (possibly several at once) to one
// lipgloss style. Priority: syntax category > code > link > marker >
// emphasis bits.
type StyleFor func(SpanStyle) lipgloss.Style

// ToANSI renders lines to a display string.
func ToANSI(lines []Line, styleFor StyleFor) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, s := range l.Spans {
			if s.Text == "" {
				continue
			}
			b.WriteString(styleFor(s.Style).Render(s.Text))
		}
	}
	return b.String()
}

// ToPlain renders lines as plain text (no styling).
func ToPlain(lines []Line) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Plain())
	}
	return b.String()
}
