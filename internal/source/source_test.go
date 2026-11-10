package source

import (
	"context"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// fakeSource is a minimal Source for registry tests. Ids deliberately
// avoid real packages; a duplicate Register of the same id panics, so
// each test uses fresh ids and unregisters in cleanup.
type fakeSource struct {
	id    string
	feeds []core.Feed
}

func (f fakeSource) ID() string                                            { return f.id }
func (f fakeSource) Feeds() []core.Feed                                    { return f.feeds }
func (f fakeSource) Fetch(context.Context, core.Feed) ([]core.Item, error) { return nil, nil }

func fake(id string, feeds ...core.Feed) Constructor {
	return func(*core.Sources) (Source, error) {
		return fakeSource{id: id, feeds: feeds}, nil
	}
}

func TestNewUnknownSourceErrors(t *testing.T) {
	registerTest(t, "test-unknown", fake("test-unknown"))
	if _, err := New("nope", core.DefaultSources()); err == nil {
		t.Fatal("expected error for unknown source id")
	}
}

func TestEnabledBuildsInConfigOrder(t *testing.T) {
	registerTest(t, "test-a", fake("test-a"))
	registerTest(t, "test-b", fake("test-b"))

	built, err := Enabled(&core.Sources{Enabled: []string{"test-b", "test-a"}})
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if len(built) != 2 || built[0].ID() != "test-b" || built[1].ID() != "test-a" {
		var ids []string
		for _, s := range built {
			ids = append(ids, s.ID())
		}
		t.Fatalf("Enabled order = %v, want [test-b test-a]", ids)
	}
}

func TestEnabledUnknownIdErrorsNamingCulprit(t *testing.T) {
	registerTest(t, "test-a", fake("test-a"))
	_, err := Enabled(&core.Sources{Enabled: []string{"test-a", "nope"}})
	if err == nil {
		t.Fatal("expected error for enabled-but-unknown id")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error should name the culprit id, got: %v", err)
	}
}

// registerTest registers ctor under id and removes it when the test
// ends, keeping the global registry clean between tests.
func registerTest(t *testing.T, id string, ctor Constructor) {
	t.Helper()
	Register(id, ctor)
	t.Cleanup(func() {
		mu.Lock()
		delete(registry, id)
		mu.Unlock()
	})
}
