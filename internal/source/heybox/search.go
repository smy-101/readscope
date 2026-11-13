package heybox

// search.go — the circle-name search behind `readscope heybox add`
// (ticket 02): the one heybox endpoint the smoke measured as
// UNSIGNED (07: /bbs/app/api/search/topic, q/offset/limit only —
// heybox-cli's observation too), which doubles as the data plane's
// free probe: when the signed shape ever breaks, this leg still
// answers, so the failure isolates the signature. The CLI is the
// only caller; the TUI grows its own door later (spec's out-of-scope
// list).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/smy-101/readscope/internal/core"
)

// searchTopicPath is the search endpoint's path (smoke 07).
const searchTopicPath = "/bbs/app/api/search/topic"

// searchLimit asks for ten candidates — the smoke probed with five,
// ten fills a pick list without paging; the parameter is a page size,
// not protocol shape, and the endpoint honors it plainly.
const searchLimit = 10

// Topic is one circle candidate from the search: what the picker
// needs (topic_id, name) and what the terminal line shows (ShowDesc,
// the host's own "热度: 298.4万" string — displayed verbatim rather
// than re-derived, so the host's formatting stays the truth).
type Topic struct {
	TopicID  int64
	Name     string
	ShowDesc string
}

// SearchTopics runs one circle-name search. The result rides the
// host's relevance order (heat-flavored) — the first candidate is
// the best guess. Degenerate entries (no topic_id or no name) skip
// silently: the search family is best-effort listing, and a
// half-formed candidate is not a circle anyone can subscribe.
func (s *Source) SearchTopics(ctx context.Context, q string) ([]Topic, error) {
	res := s.tr.GetUnsigned(ctx, "topic-search", searchTopicPath, url.Values{
		"q":      {q},
		"offset": {"0"},
		"limit":  {fmt.Sprintf("%d", searchLimit)},
	})
	if res.Err != nil {
		return nil, fmt.Errorf("search %q: %w", q, res.Err)
	}
	var found struct {
		Topics []struct {
			TopicID  int64  `json:"topic_id"`
			Name     string `json:"name"`
			ShowDesc string `json:"show_desc"`
		} `json:"topics"`
	}
	if err := json.Unmarshal(res.Result, &found); err != nil {
		return nil, fmt.Errorf("parse search %q: not heybox JSON: %w", q, err)
	}
	topics := make([]Topic, 0, len(found.Topics))
	for _, t := range found.Topics {
		if t.TopicID <= 0 || t.Name == "" {
			continue
		}
		topics = append(topics, Topic{
			TopicID:  t.TopicID,
			Name:     t.Name,
			ShowDesc: t.ShowDesc,
		})
	}
	return topics, nil
}

// Subscribe appends one circle to the in-memory subscription list —
// the CLI's write path after the pick, one call away from
// srcs.Save(). A topic already in the list is a no-op report
// (added=false), never a duplicate row.
func Subscribe(srcs *core.Sources, t Topic) bool {
	for _, c := range srcs.HeyboxCircles() {
		if c.TopicID == t.TopicID {
			return false
		}
	}
	if srcs.Heybox == nil {
		srcs.Heybox = &core.HeyboxConfig{}
	}
	srcs.Heybox.Circles = append(srcs.Heybox.Circles, core.HeyboxCircle{
		Name:    t.Name,
		TopicID: t.TopicID,
	})
	return true
}
