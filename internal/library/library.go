// Package library owns the 书库 domain (GLOSSARY: the EPUB directory
// on disk, not the screen): recursive scan of the library directory
// is the shelf's source of truth — a parseable book upserts its
// metadata row, a vanished file delists (cascading reading state
// away), and a book the parse layer refuses is collected, typed, for
// the bookshelf's ⚠ marker and doctor's counts. Scan results are
// deliberately not persisted: every bookshelf entry and every doctor
// run scans fresh, so what the caller holds is always the disk's
// verdict (zero migration, no second code path).
package library

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/store"
)

// Dir resolves the configured library directory to an absolute path:
// a leading ~ expands to the user's home, anything else passes
// through after filepath.Abs. The directory need not exist yet — a
// fresh install scans empty, not broken.
func Dir(cfg *core.Config) (string, error) {
	dir := cfg.BookDir
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	return filepath.Abs(dir)
}

// FailedBook is one file the parse layer refused — the two hard
// failures (epub.ErrInvalidZip, epub.ErrDRMUnsupported). The error is
// carried whole so consumers can errors.Is their way to a typed
// message.
type FailedBook struct {
	Path string
	Err  error
}

// Result is one scan's outcome. New/Updated/Delisted count database
// writes (an unchanged library scans to zeros); Warned counts books
// that opened with degradations; Failed lists the refused files in
// walk order — a failed file is NOT delisted, its row (if any)
// survives with reading state intact.
type Result struct {
	Scanned  int
	New      int
	Updated  int
	Delisted int
	Warned   int
	Failed   []FailedBook
}

// Scan walks dir recursively for *.epub files, upserts every book
// that opens, delists rows whose files vanished, and collects the
// rest. It is synchronous and library-scale fast (one zip central
// directory per book, no content reads). A missing dir is an empty
// result, not an error; an unreadable subdirectory fails the walk
// (the user should hear about it).
func Scan(ctx context.Context, st *store.Store, dir string) (Result, error) {
	var res Result
	var seen []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".epub") {
			return nil
		}
		res.Scanned++
		seen = append(seen, path)
		book, err := epub.Open(path)
		if err != nil {
			res.Failed = append(res.Failed, FailedBook{Path: path, Err: err})
			return nil
		}
		title := book.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		_, outcome, err := st.UpsertBook(ctx, path, title,
			strings.Join(book.Authors, ", "), book.Language, time.Now().UTC())
		if book.Close(); err != nil {
			return err
		}
		if len(book.Warnings) > 0 {
			res.Warned++
		}
		switch outcome {
		case store.BookInserted:
			res.New++
		case store.BookUpdated:
			res.Updated++
		}
		return nil
	})
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	res.Delisted, err = st.DelistMissing(ctx, seen)
	return res, err
}
