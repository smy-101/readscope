package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/zhihu"
)

const zhihuUsage = "usage: readscope zhihu <probe|login> [flags]\n" +
	"  probe   walk the API endpoints, report verdicts, save raw samples\n" +
	"  login   paste a browser Cookie header to enable the logged-in endpoints"

// runZhihu dispatches `readscope zhihu <subcommand>`: probe (ticket
// 01, the M4 data-plane tracer) and login (ticket 04, the login
// ceremony — prompt, paste, validate, persist 0600). Same cookie
// file, same semantics as the TUI's Ctrl+U dialog.
func runZhihu(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, zhihuUsage)
		return 2
	}
	switch args[0] {
	case "probe":
		return runZhihuProbe(args[1:])
	case "login":
		return runZhihuLogin(args[1:])
	default:
		fmt.Fprintln(os.Stderr, zhihuUsage)
		return 2
	}
}

func runZhihuProbe(args []string) int {
	fs := flag.NewFlagSet("zhihu probe", flag.ContinueOnError)
	cookieFlag := fs.String("cookie", "", "cookie file: raw Cookie header text (default: <config>/zhihu.cookies)")
	outFlag := fs.String("out", ".scratch/m4-zhihu/samples", "directory for raw response samples")
	qidFlag := fs.String("qid", "", "override question id (else chained from the hot list)")
	storyFlag := fs.String("story", "", "override daily story id (else chained from the daily list)")
	aidFlag := fs.String("aid", "", "override answer id for the comment probe (else chained from answers)")
	if err := fs.Parse(reorderFlags(args[1:])); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "readscope: unexpected argument %q\n%s\n", fs.Arg(0), zhihuUsage)
		return 2
	}

	cookiePath := *cookieFlag
	if cookiePath == "" {
		dir, err := core.Dir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
			return 1
		}
		cookiePath = filepath.Join(dir, "zhihu.cookies")
	}

	p := &zhihu.Probe{
		Hosts:  zhihu.ProdHosts,
		OutDir: *outFlag,
		Args:   zhihu.ProbeArgs{QID: *qidFlag, StoryID: *storyFlag, Aid: *aidFlag},
	}
	// One base override for debugging (and any future fake-server
	// scenario): every host root replaced by READSCOPE_ZHIHU_BASE.
	if v := os.Getenv("READSCOPE_ZHIHU_BASE"); v != "" {
		p.Hosts = zhihu.Hosts{API: v, Daily: v, Web: v}
	}
	if header, _, err := zhihu.LoadCookie(cookiePath); err == nil {
		p.CookieHeader = header
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "readscope: read cookie: %v\n", err)
		return 1
	} // missing file: the anonymous walk is a valid outcome, not an error

	results := p.Run(context.Background(), os.Stdout)
	if zhihu.Blocked(results) {
		return 1
	}
	return 0
}

// runZhihuLogin is the CLI half of the login ceremony: print the
// DevTools steps, read the pasted header (all of stdin — a wrapped
// multi-line paste joins back into one header), validate (z_c0
// aboard or refuse), persist 0600. The TUI's Ctrl+U dialog runs the
// same SaveCookie seam; values never echo back.
func runZhihuLogin(args []string) int {
	fs := flag.NewFlagSet("zhihu login", flag.ContinueOnError)
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "readscope: unexpected argument %q\n%s\n", fs.Arg(0), zhihuUsage)
		return 2
	}
	cat := loadCatalogLenient()
	path, err := zhihu.CookiePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}

	fmt.Println(cat.Get("zhihu.cli_login_banner"))
	fmt.Println(cat.Get("zhihu.cli_login_prompt"))
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: read stdin: %v\n", err)
		return 1
	}
	info, err := zhihu.SaveCookie(path, string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	dc0 := "✗"
	if info.HasDC0 {
		dc0 = "✓"
	}
	fmt.Println(cat.Get("zhihu.cli_login_saved", path, info.KeyCount, dc0))
	fmt.Println(cat.Get("zhihu.cli_login_hint2"))
	return 0
}
