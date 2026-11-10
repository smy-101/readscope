package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/render"
	"github.com/smy-101/readscope/internal/theme"
)

// runRender implements `readscope render <file|URL>`: the render pipeline's
// debug outlet — same code path the TUI will use, visible without a TUI.
func runRender(args []string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	widthFlag := fs.Int("width", 0, "layout width in columns (default: terminal width, else 80)")
	colorFlag := fs.Bool("color", false, "force ANSI colors")
	plainFlag := fs.Bool("plain", false, "force plain text (no ANSI)")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return 2
	}
	target := fs.Arg(0)
	if target == "" {
		fmt.Fprintln(os.Stderr, "usage: readscope render <file.html|URL> [--width N] [--color] [--plain]")
		return 2
	}

	cfg, err := core.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	cat, err := i18n.Load(cfg.Language)
	if err != nil {
		cat, _ = i18n.Load(i18n.Default)
	}

	src, err := readSource(target, 15*time.Second)
	if err != nil {
		if fe, ok := err.(fetchError); ok {
			fmt.Fprintf(os.Stderr, "readscope: %s\n", cat.Get(fe.key, fe.target, fe.err))
		} else {
			fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		}
		return 1
	}
	doc, err := render.ParseHTML(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %s\n", cat.Get("errors.render_parse", err))
		return 1
	}

	width := *widthFlag
	if width <= 0 {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			width = w
		} else {
			width = 80
		}
	}
	lines := render.Layout(doc, width)

	switch {
	case *plainFlag:
		fmt.Println(render.ToPlain(lines))
	case *colorFlag:
		// Piped stdout would otherwise downgrade to no-color; --color forces.
		lipgloss.SetColorProfile(termenv.ANSI256)
		th, _ := theme.Load(cfg.Theme)
		fmt.Println(render.ToANSI(lines, th.StyleFor))
	default:
		if term.IsTerminal(int(os.Stdout.Fd())) {
			th, _ := theme.Load(cfg.Theme)
			fmt.Println(render.ToANSI(lines, th.StyleFor))
		} else {
			fmt.Println(render.ToPlain(lines))
		}
	}
	return 0
}

// reorderFlags moves flag tokens (and the --width value) ahead of
// positional arguments — flag.Parse stops at the first positional, and
// users write "render file.html --color".
func reorderFlags(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") &&
				(strings.HasSuffix(a, "width")) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			rest = append(rest, a)
		}
	}
	return append(flags, rest...)
}

// fetchError carries which i18n key applies.
type fetchError struct {
	key    string // errors.render_read | errors.render_fetch
	target string
	err    error
}

func (e fetchError) Error() string { return e.err.Error() }

func readSource(target string, timeout time.Duration) (string, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		client := &http.Client{Timeout: timeout}
		req, err := http.NewRequest("GET", target, nil)
		if err != nil {
			return "", fetchError{"errors.render_fetch", target, err}
		}
		req.Header.Set("User-Agent", "readscope/"+Version)
		resp, err := client.Do(req)
		if err != nil {
			return "", fetchError{"errors.render_fetch", target, err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fetchError{"errors.render_fetch", target, fmt.Errorf("HTTP %d", resp.StatusCode)}
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return "", fetchError{"errors.render_fetch", target, err}
		}
		return string(body), nil
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return "", fetchError{"errors.render_read", target, err}
	}
	return string(raw), nil
}
