// detail.go: the hn half of the DetailSource extension (ticket 03).
// One GET to /items/<storyID> returns the story with its entire
// comment tree nested in children; Detail flattens that tree in DFS
// pre-order (floors in payload order, each followed by its subtree,
// parents strictly before children) so the store can persist it as a
// flat slice and the UI can rebuild or flatten the forest later.

package hn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// itemIDPrefix is the deterministic list-layer id prefix Detail
// reverses to recover the story id.
const itemIDPrefix = ID + ":algolia:"

// Detail implements source.DetailSource: the story's whole comment
// tree in one request. The story node itself is not returned — its
// body already lives in the list snapshot; this call is about the
// comments. Deleted comments survive as placeholder nodes (no author,
// no text) so their children's parent links still resolve.
func (s *Source) Detail(ctx context.Context, it core.Item) (core.Content, error) {
	storyID, ok := strings.CutPrefix(it.ID, itemIDPrefix)
	if !ok {
		return core.Content{}, fmt.Errorf("detail: %q is not an hn story id", it.ID)
	}
	url := s.base + "/items/" + storyID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return core.Content{}, fmt.Errorf("detail %s: %w", storyID, err)
	}
	req.Header.Set("User-Agent", source.UserAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return core.Content{}, fmt.Errorf("detail %s: %w", storyID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return core.Content{}, fmt.Errorf("detail %s: HTTP %s", storyID, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return core.Content{}, fmt.Errorf("detail %s: %w", storyID, err)
	}
	var root itemNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return core.Content{}, fmt.Errorf("detail %s: not Algolia JSON: %w", storyID, err)
	}
	c := core.Content{Comments: []core.Comment{}}
	collectComments(&root, "", &c.Comments)
	return c, nil
}

// itemNode is one node of Algolia's /items payload. The story sits at
// the root (points on it, text holds a text post's HTML); comments
// nest through children, their parent_id pointing at the immediate
// parent — the story id for top-level floors. Deleted comments carry
// deleted=true and nothing else but children. Comment points are
// always null in this endpoint (verified against the live API), so
// Comment.Score stays nil for hn by construction.
type itemNode struct {
	ID         int64      `json:"id"`
	Author     string     `json:"author"`
	Text       *string    `json:"text"` // null for link posts and deleted nodes
	Points     *int       `json:"points"`
	CreatedAtI int64      `json:"created_at_i"`
	Deleted    bool       `json:"deleted"`
	ParentID   *int64     `json:"parent_id"`
	Children   []itemNode `json:"children"`
}

// collectComments flattens one node's descendants into out, DFS
// pre-order. The root call passes the story node with parentID "",
// so its direct children — the top-level floors — keep ParentID "";
// each recursion passes the comment id the subtree hangs under. The
// walk, not the payload's parent_id field, is the source of truth for
// the link: floors have parent_id = story id in the payload, which
// must become "" in the cache.
func collectComments(n *itemNode, parentID string, out *[]core.Comment) {
	for i := range n.Children {
		c := &n.Children[i]
		cm := core.Comment{ID: strconv.FormatInt(c.ID, 10), ParentID: parentID}
		if !c.Deleted {
			cm.Author = c.Author
			if c.Text != nil {
				cm.Text = *c.Text
			}
			if c.CreatedAtI != 0 {
				t := time.Unix(c.CreatedAtI, 0).UTC()
				cm.CreatedAt = &t
			}
		}
		*out = append(*out, cm)
		collectComments(c, cm.ID, out)
	}
}
