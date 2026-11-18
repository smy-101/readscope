package main

// add.go — the `readscope add` subcommand (ADR #10): copy one EPUB
// into the configured library directory. The scan is the shelf's
// only writer, so add is deliberately just a copy plus a report of
// what the parse layer sees; a same-name conflict is refused naming
// the existing path (zero writes — replacing a book is rm + add,
// the same 删文件即下架 gesture the shelf itself rides).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/library"
)

// runAdd implements `readscope add <file>`. Exit codes: 0 copied
// (the report may still carry parse warnings or the typed refusal of
// a broken book — the copy stands, the shelf's ⚠ explains), 1 the
// file is missing / a same-name conflict / the copy failed, 2 usage.
func runAdd(args []string) int {
	cat := loadCatalogLenient()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, cat.Get("add.usage"))
		return 2
	}
	src := args[0]
	fi, err := os.Stat(src)
	if err != nil || fi.IsDir() {
		fmt.Fprintln(os.Stderr, cat.Get("add.missing", src))
		return 1
	}
	cfg, err := core.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	dir, err := library.Dir(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %s\n", cat.Get("add.err_copy", err))
		return 1
	}
	dst := filepath.Join(dir, filepath.Base(src))
	if _, err := os.Stat(dst); err == nil {
		// The refusal is the ceremony's answer, not an error dump —
		// stdout like the dup-pick line, stderr stays for failures.
		fmt.Println(cat.Get("add.exists", dst))
		return 1
	}
	if err := copyFile(dst, src); err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %s\n", cat.Get("add.err_copy", err))
		return 1
	}

	// Report what landed: title and warnings when it opens, the
	// typed refusal when it does not (the copy stands either way —
	// deleting the file is the uninstall gesture).
	if book, err := epub.Open(dst); err != nil {
		fmt.Println(cat.Get("add.copied_broken", dst, addErrText(cat, err)))
	} else {
		title, warns := book.Title, len(book.Warnings)
		book.Close()
		if warns > 0 {
			fmt.Println(cat.Get("add.copied_dirty", dst, title, warns))
		} else {
			fmt.Println(cat.Get("add.copied", dst, title))
		}
	}
	return 0
}

// addErrText maps the parse layer's two hard failures to their
// localized lines (the bookshelf's Enter explanation is the other
// consumer of this mapping — two occurrences, the tolerance line).
func addErrText(cat *i18n.Catalog, err error) string {
	switch {
	case errors.Is(err, epub.ErrDRMUnsupported):
		return cat.Get("books.err_drm")
	case errors.Is(err, epub.ErrInvalidZip):
		return cat.Get("books.err_zip")
	default:
		return cat.Get("books.err_open", err)
	}
}

// copyFile streams src to dst and removes a partial dst on failure —
// a half-written book must not linger as a shelf-shaped surprise.
func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
