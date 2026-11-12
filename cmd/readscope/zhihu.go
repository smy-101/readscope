package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/zhihu"
)

const zhihuUsage = "usage: readscope zhihu probe [-cookie <file>] [-out <dir>] [-qid <id>] [-story <id>] [-aid <id>]"

// runZhihu dispatches `readscope zhihu <subcommand>`. Ticket 01 lands
// probe — the M4 data-plane tracer; the login ceremony (ticket 04)
// adds `zhihu login` here later.
func runZhihu(args []string) int {
	if len(args) == 0 || args[0] != "probe" {
		fmt.Fprintln(os.Stderr, zhihuUsage)
		return 2
	}
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
