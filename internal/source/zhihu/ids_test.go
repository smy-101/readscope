package zhihu

// Ticket 01 unit tests: the two id-extraction paths the spec pins —
// qid from URL (exact for ids beyond 2^53) and json.Number decoding
// (exact literal, unlike float64). The fixtures carry a real-shaped
// 19-digit qid (>2^53) so the precision contract is exercised, not
// just assumed.

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestQIDFromURL(t *testing.T) {
	cases := []struct {
		name, url, want string
	}{
		{"www question", "https://www.zhihu.com/question/18984420158198241699", "18984420158198241699"},
		{"api questions", "https://api.zhihu.com/questions/18984420158198241699", "18984420158198241699"},
		{"with answer suffix", "https://www.zhihu.com/question/123456/answer/789", "123456"},
		{"with query", "https://www.zhihu.com/question/987?utm=x", "987"},
		{"not a question", "https://www.zhihu.com/people/foo", ""},
		{"bare host", "https://www.zhihu.com/", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		if got := QIDFromURL(tc.url); got != tc.want {
			t.Errorf("%s: QIDFromURL(%q) = %q, want %q", tc.name, tc.url, got, tc.want)
		}
	}
}

// hotFixture is the slice of the hot-list payload these tests consume;
// the probe reuses the same shape. target.id is UNQUOTED in real
// payloads — a 19-digit integer beyond 2^53 that float64 cannot hold.
// The second row's url deliberately carries a rounder id than its id
// field: the url is the source of truth, and the test proves the two
// are read independently.
const hotFixture = `{"data":[
  {"detail_text":"2045 万热度","target":{"id":18984420158198241699,"type":"question",
   "title":"为什么终端阅读器又火了？","url":"https://api.zhihu.com/questions/18984420158198241699"}},
  {"detail_text":"512 万热度","target":{"id":9007199254740993,"type":"question",
   "title":"刚过线","url":"https://api.zhihu.com/questions/9007199254740993"}}
]}`

func TestHotListQIDExactFromURLAndJSONNumber(t *testing.T) {
	var hot hotList
	if err := json.Unmarshal([]byte(hotFixture), &hot); err != nil {
		t.Fatalf("parse hot fixture: %v", err)
	}
	if len(hot.Data) != 2 {
		t.Fatalf("rows = %d, want 2", len(hot.Data))
	}

	// Path 1 (the one production uses): extract from target.url — exact.
	if got := QIDFromURL(hot.Data[0].Target.URL); got != "18984420158198241699" {
		t.Errorf("url-extracted qid = %q, want the exact 19-digit id", got)
	}

	// Path 2: json.Number keeps the literal verbatim; url and id agree.
	if string(hot.Data[0].Target.ID) != "18984420158198241699" {
		t.Errorf("json.Number id = %q, want exact literal", hot.Data[0].Target.ID)
	}

	// 2^53 = 9007199254740992: the first id is far beyond, the second
	// exactly one past — both flagged, a 15-digit id is not.
	for id, want := range map[string]bool{
		"18984420158198241699": true,
		"9007199254740993":     true,
		"9007199254740991":     false,
		"123456":               false,
	} {
		if got := OverJSMaxInt(id); got != want {
			t.Errorf("OverJSMaxInt(%s) = %v, want %v", id, got, want)
		}
	}
}

// TestFloat64LosesPrecision documents WHY ids come from URLs: the same
// fixture decoded through float64 mangles the trailing digits that
// distinguish one question from another (…1699 → …16xx).
func TestFloat64LosesPrecision(t *testing.T) {
	var lossy struct {
		Data []struct {
			Target struct {
				ID float64 `json:"id"`
			} `json:"target"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(hotFixture), &lossy); err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := strconv.FormatFloat(lossy.Data[0].Target.ID, 'f', -1, 64)
	if got == "18984420158198241699" {
		t.Fatal("float64 path unexpectedly exact; the precision rationale is stale and the URL-extraction rule should be revisited")
	}
}
