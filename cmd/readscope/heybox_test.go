package main

// heybox_test.go — the 加圈 CLI (ticket 02) end to end against a
// fake search host: candidates listed with their heat, the numbered
// pick written into a tempdir sources.toml (TOML round-trip), the
// duplicate/cancel/no-match/no-write semantics, and the re-prompt on
// a bad number. Stdin is a reader the test scripts; stdout is
// captured through a pipe where its wording matters.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const addSearchFixture = `{"msg":"","result":{"topics":[
  {"topic_id":568547,"name":"黑神话：悟空","show_desc":"热度: 298.4万","hot":{"raw_hot_value":2984176}},
  {"topic_id":709226,"name":"黑神话：钟馗","show_desc":"热度: 5.1万","hot":{"raw_hot_value":51060}}
],"version":"1.0"},"status":"ok"}`

// addEnv points one add-run at a fake host and a tempdir config.
func addEnv(t *testing.T, srvURL string) (sourcesPath string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", cfgDir)
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	t.Setenv("READSCOPE_HEYBOX_BASE", srvURL)
	t.Setenv("READSCOPE_HEYBOX_MINGAP", "0") // tests run flat out
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "0123456789abcdef0123456789abcdef")
	return filepath.Join(cfgDir, "sources.toml")
}

// fakeSearchHost serves the search fixture and counts the dials.
func fakeSearchHost(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	dials := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dials++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, addSearchFixture)
	}))
	t.Cleanup(srv.Close)
	return srv, &dials
}

// captureStdout runs fn with os.Stdout piped, returning what it
// printed. The CLI's output here is a few hundred bytes — far under
// any pipe buffer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	raw, _ := io.ReadAll(r)
	return string(raw)
}

func TestHeyboxAddPicksAndWrites(t *testing.T) {
	srv, dials := fakeSearchHost(t)
	path := addEnv(t, srv.URL)

	var code int
	got := captureStdout(t, func() {
		code = runHeyboxAdd([]string{"黑神话"}, strings.NewReader("1\n"))
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	// The candidate list: numbered, named, with the host's own heat
	// string — the triage line the ticket asks for.
	for _, want := range []string{"1. 黑神话：悟空 · 热度: 298.4万", "2. 黑神话：钟馗 · 热度: 5.1万"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing candidate %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "568547") {
		t.Errorf("the added-line should name the topic id:\n%s", got)
	}
	// TOML round-trip: the picked circle is on disk with its id.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sources.toml after add: %v", err)
	}
	for _, want := range []string{"[[heybox.circles]]", "name = '黑神话：悟空'", "topic_id = 568547"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("sources.toml missing %q:\n%s", want, raw)
		}
	}
	// The enabled default list survived the write.
	if !strings.Contains(string(raw), "'heybox'") {
		t.Errorf("sources.toml lost the enabled list:\n%s", raw)
	}
	if *dials != 1 {
		t.Errorf("search dialed %d times, want 1", *dials)
	}
}

func TestHeyboxAddDuplicateIsNoWrite(t *testing.T) {
	srv, _ := fakeSearchHost(t)
	path := addEnv(t, srv.URL)

	// First add: circle 568547 lands. Second add picking the SAME
	// circle: exit 0, still exactly one circle.
	if code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("1\n")); code != 0 {
		t.Fatalf("first add exit = %d, want 0", code)
	}
	if code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("1\n")); code != 0 {
		t.Fatalf("duplicate add exit = %d, want 0 (a no-op success)", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "[[heybox.circles]]"); n != 1 {
		t.Fatalf("%d circle tables after a duplicate add, want 1:\n%s", n, raw)
	}
	// A different circle still appends (the second candidate).
	if code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("2\n")); code != 0 {
		t.Fatalf("second circle add exit = %d, want 0", code)
	}
	raw, _ = os.ReadFile(path)
	if n := strings.Count(string(raw), "[[heybox.circles]]"); n != 2 {
		t.Fatalf("%d circle tables, want 2:\n%s", n, raw)
	}
}

func TestHeyboxAddCancelWritesNothing(t *testing.T) {
	srv, dials := fakeSearchHost(t)
	path := addEnv(t, srv.URL)

	if code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("q\n")); code != 0 {
		t.Fatalf("cancel exit = %d, want 0", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a cancelled add must not create sources.toml (err=%v)", err)
	}
	// EOF without a pick cancels too, never crashing.
	if code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("")); code != 0 {
		t.Fatalf("EOF exit = %d, want 0", code)
	}
	if *dials != 2 {
		t.Errorf("search dialed %d times, want 2 (one per run)", *dials)
	}
}

func TestHeyboxAddBadPickReprompts(t *testing.T) {
	srv, _ := fakeSearchHost(t)
	path := addEnv(t, srv.URL)

	// Garbage, then out-of-range, then the real pick: one ceremony,
	// one write.
	code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("abc\n99\n2\n"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 after re-prompts", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "topic_id = 709226") {
		t.Fatalf("the final pick (circle 2) must be the one written:\n%s", raw)
	}
}

func TestHeyboxAddNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"msg":"","result":{"topics":[]},"status":"ok"}`)
	}))
	t.Cleanup(srv.Close)
	path := addEnv(t, srv.URL)

	if code := runHeyboxAdd([]string{"不存在的圈"}, strings.NewReader("1\n")); code != 1 {
		t.Fatalf("no-results exit = %d, want 1", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a fruitless search must not write sources.toml")
	}
}

func TestHeyboxAddNetworkFailureWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // a dead address: the connection-refused family
	path := addEnv(t, srv.URL)

	if code := runHeyboxAdd([]string{"黑神话"}, strings.NewReader("1\n")); code != 1 {
		t.Fatalf("network-failure exit = %d, want 1", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a failed search must not write sources.toml")
	}
}

func TestHeyboxUsage(t *testing.T) {
	if code := runHeybox(nil); code != 2 {
		t.Fatalf("bare heybox exit = %d, want 2", code)
	}
	if code := runHeybox([]string{"dance"}); code != 2 {
		t.Fatalf("unknown subcommand exit = %d, want 2", code)
	}
	if code := runHeyboxAdd(nil, strings.NewReader("1\n")); code != 2 {
		t.Fatalf("add without keyword exit = %d, want 2", code)
	}
}
