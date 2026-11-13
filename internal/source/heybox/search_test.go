package heybox

// search_test.go — the unsigned circle search (ticket 02) against a
// fake host: the request carries ONLY its own params (no signature
// triple, no common web set — the measured 07 shape, pinned so
// nobody "unifies" the two transport paths), the candidates parse
// with their heat strings, degenerate rows skip, and failures ride
// the standard error family. The end-to-end add flow (pick → TOML
// write) lives in cmd/readscope's tests.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// searchFixture is the smoke's 07 answer, trimmed to the fields the
// picker reads.
const searchFixture = `{"msg":"","result":{"topics":[
  {"topic_id":568547,"name":"黑神话：悟空","show_desc":"热度: 298.4万","hot":{"raw_hot_value":2984176}},
  {"topic_id":709226,"name":"黑神话：钟馗","show_desc":"热度: 5.1万","hot":{"raw_hot_value":51060}},
  {"topic_id":0,"name":"坏条目无id","show_desc":"","hot":{}},
  {"topic_id":123,"name":"","show_desc":"坏条目无名","hot":{}}
],"version":"1.0"},"status":"ok"}`

// fakeSearch answers the search endpoint, recording each request's
// full query. It deliberately does NOT check a signature — the test
// asserts the absence of one instead.
func fakeSearch(t *testing.T, body string) (*Source, *[]url.Values) {
	t.Helper()
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != searchTopicPath {
			t.Errorf("search dialed %q, want %q", r.URL.Path, searchTopicPath)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		got = append(got, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newSourceAt(srv.URL, WithMinGap(0)), &got
}

func TestSearchTopicsUnsignedShape(t *testing.T) {
	src, got := fakeSearch(t, searchFixture)
	topics, err := src.SearchTopics(t.Context(), "黑神话")
	if err != nil {
		t.Fatalf("SearchTopics: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("fake saw %d requests, want 1", len(*got))
	}
	q := (*got)[0]
	// The measured shape: ONLY q/offset/limit. A signature triple or
	// a common-set param here would be a different request than the
	// one the smoke greened.
	for _, forbidden := range []string{"hkey", "_time", "nonce", "os_type", "version", "x_app", "device_id"} {
		if _, ok := q[forbidden]; ok {
			t.Errorf("unsigned search carried %s=%v — the measured 07 shape has none", forbidden, q[forbidden])
		}
	}
	if q.Get("q") != "黑神话" || q.Get("offset") != "0" || q.Get("limit") != "10" {
		t.Errorf("search params = %v, want q/offset=0/limit=10", q)
	}

	// Two clean candidates parse; the degenerate rows (no id, no
	// name) skip without failing the page.
	if len(topics) != 2 {
		t.Fatalf("got %d topics, want 2 (degenerate rows skipped)", len(topics))
	}
	first := topics[0]
	if first.TopicID != 568547 || first.Name != "黑神话：悟空" || first.ShowDesc != "热度: 298.4万" || first.Hot != 2984176 {
		t.Errorf("topics[0] = %+v, want the host's own row", first)
	}
	if topics[1].TopicID != 709226 {
		t.Errorf("topics[1].TopicID = %d, want 709226 (host order kept)", topics[1].TopicID)
	}
}

func TestSearchTopicsNoResultsIsEmpty(t *testing.T) {
	src, _ := fakeSearch(t, `{"msg":"","result":{"topics":[]},"status":"ok"}`)
	topics, err := src.SearchTopics(t.Context(), "不存在的圈子名字")
	if err != nil {
		t.Fatalf("empty search must not error: %v", err)
	}
	if len(topics) != 0 {
		t.Fatalf("got %d topics, want 0", len(topics))
	}
}

func TestSearchTopicsRefusalIsPlainError(t *testing.T) {
	src, _ := fakeSearch(t, `{"msg":"","result":{},"status":"show_captcha"}`)
	if _, err := src.SearchTopics(t.Context(), "黑神话"); err == nil {
		t.Fatal("a refused search must error, not answer empty")
	}
}

func TestSubscribeAppendsAndDedupes(t *testing.T) {
	srcs := core.DefaultSources()
	if got := srcs.HeyboxCircles(); len(got) != 0 {
		t.Fatalf("fresh install: %d circles, want 0", len(got))
	}
	if !Subscribe(srcs, Topic{TopicID: 568547, Name: "黑神话：悟空"}) {
		t.Fatal("first subscribe must add")
	}
	if !Subscribe(srcs, Topic{TopicID: 42, Name: "塞尔达传说"}) {
		t.Fatal("second subscribe must add")
	}
	// Same topic under a different name is still the same circle:
	// the feed key (and its paging depth) would collide.
	if Subscribe(srcs, Topic{TopicID: 568547, Name: "黑神话：悟空(改名)"}) {
		t.Fatal("a duplicate topic_id must not append")
	}
	circles := srcs.HeyboxCircles()
	if len(circles) != 2 || circles[0].TopicID != 568547 || circles[1].TopicID != 42 {
		t.Fatalf("circles = %+v, want declaration order [568547 42]", circles)
	}
}
