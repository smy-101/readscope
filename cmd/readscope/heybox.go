package main

// heybox.go — the heybox subcommands: `add`, the 加圈 ceremony
// (GLOSSARY: one-shot, redoable, same family as zhihu's login).
// keyword search → numbered candidates → pick → sources.toml
// write-back; the running TUI picks the new circle up on next start
// (the TOML is always the fallback door: hand-edit, restart, done).

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/heybox"
)

const heyboxUsage = "usage: readscope heybox add <关键词…>\n" +
	"  add   search circles by keyword, pick one, subscribe it into sources.toml"

// runHeybox dispatches `readscope heybox <subcommand>`.
func runHeybox(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, heyboxUsage)
		return 2
	}
	switch args[0] {
	case "add":
		return runHeyboxAdd(args[1:], os.Stdin)
	default:
		fmt.Fprintf(os.Stderr, "readscope: unknown heybox subcommand %q\n%s\n", args[0], heyboxUsage)
		return 2
	}
}

// runHeyboxAdd implements `readscope heybox add <关键词…>`: search
// the host's circle directory (unsigned, ticket 02), list the
// candidates with their heat, read one numbered pick off in, append
// the circle to sources.toml. Extra words join into one keyword —
// `add black myth` and `add "black myth"` search the same string.
// Exit codes: 0 added / already-subscribed / cancelled (nothing
// written), 1 nothing to pick or the search/save failed, 2 usage.
// A bad pick re-prompts rather than failing — the ceremony is
// one-shot, not one-chance.
func runHeyboxAdd(args []string, in io.Reader) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, heyboxUsage)
		return 2
	}
	keyword := strings.Join(args, " ")
	cat := loadCatalogLenient()

	srcs := loadSources()
	if srcs == nil {
		return 1
	}
	// Constructing the source validates the existing circle table
	// (a broken hand edit refuses before any dial) and hands the
	// search a transport — env overrides honored, so a fake server
	// can stand in for tests and debug runs. The concrete type is
	// asserted back out: SearchTopics is this package's own door,
	// not part of the source.Source seam.
	base, err := heybox.New(srcs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	src, ok := base.(*heybox.Source)
	if !ok {
		fmt.Fprintf(os.Stderr, "readscope: heybox: unexpected source implementation %T\n", base)
		return 1
	}

	fmt.Println(cat.Get("heybox.cli_searching", keyword))
	topics, err := src.SearchTopics(context.Background(), keyword)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	if len(topics) == 0 {
		fmt.Println(cat.Get("heybox.cli_none", keyword))
		return 1
	}
	for i, t := range topics {
		if t.ShowDesc != "" {
			fmt.Println(cat.Get("heybox.cli_list_line", i+1, t.Name, t.ShowDesc))
		} else {
			fmt.Println(cat.Get("heybox.cli_list_line_plain", i+1, t.Name))
		}
	}

	reader := bufio.NewReader(in)
	for {
		fmt.Print(cat.Get("heybox.cli_pick"))
		line, err := reader.ReadString('\n')
		ans := strings.TrimSpace(line)
		if ans == "" && err != nil {
			// EOF without a pick: the user closed the door — cancel,
			// never crash and never write.
			fmt.Println()
			fmt.Println(cat.Get("heybox.cli_cancel"))
			return 0
		}
		switch {
		case ans == "q" || ans == "Q":
			fmt.Println(cat.Get("heybox.cli_cancel"))
			return 0
		case ans == "":
			continue // stray enter re-prompts
		}
		n, convErr := strconv.Atoi(ans)
		if convErr != nil || n < 1 || n > len(topics) {
			fmt.Println(cat.Get("heybox.cli_bad_pick", ans, len(topics)))
			continue
		}
		picked := topics[n-1]
		if !heybox.Subscribe(srcs, picked) {
			fmt.Println(cat.Get("heybox.cli_dup", picked.Name, picked.TopicID))
			return 0
		}
		if err := srcs.Save(); err != nil {
			path, _ := core.SourcesPath()
			fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.sources_save", path, err))
			return 1
		}
		fmt.Println(cat.Get("heybox.cli_added", picked.Name, picked.TopicID))
		return 0
	}
}
