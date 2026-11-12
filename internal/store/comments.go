// comments.go: the comment cache's persistence (ticket 03). One item
// owns one comment tree, written whole (delete + insert in a single
// transaction) every time a Detail fetch comes back — repeat opens
// and background refreshes replace, never append, so no duplicate
// rows and no stale floors can survive. Tree order (parents strictly
// before children, DFS pre-order from the fetch) is carried by the
// seq column, which every whole-tree replace rewrites from zero.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

// SaveComments replaces one item's whole comment tree. The slice is
// stored verbatim in the order given (the Detail fetch's tree order);
// ParentID "" maps onto the NULL that marks a top-level floor. The
// item must already exist (comments reference items); anything else
// fails the foreign key and rolls back.
func (s *Store) SaveComments(ctx context.Context, itemID string, comments []core.Comment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("clear comments %s: %w", itemID, err)
	}
	for i, cm := range comments {
		var parent any // NULL for a top-level floor
		if cm.ParentID != "" {
			parent = cm.ParentID
		}
		var score any
		if cm.Score != nil {
			score = *cm.Score
		}
		var kids any // NULL = the source reports no child count (hn)
		if cm.NumChildren != nil {
			kids = *cm.NumChildren
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO comments
			(item_id, id, parent_id, author, text, score, created_at, seq, num_children)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			itemID, cm.ID, parent, cm.Author, cm.Text, score, cm.CreatedAt, i, kids); err != nil {
			return fmt.Errorf("store comment %s/%s: %w", itemID, cm.ID, err)
		}
	}
	// The opened marker rides the same transaction (ticket 04): an
	// empty tree still marks the item opened — that is the whole point
	// of the marker. The upsert touches fetched_at only, so the
	// question meta triple (ticket 06, written by SaveQuestionInfo)
	// survives every whole-tree replace.
	if _, err := tx.ExecContext(ctx, `INSERT INTO detail_fetch (item_id, fetched_at)
		VALUES (?, ?) ON CONFLICT(item_id) DO UPDATE SET fetched_at = excluded.fetched_at`,
		itemID, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark detail fetched %s: %w", itemID, err)
	}
	return tx.Commit()
}

// DetailFetched reports whether the item's detail layer was ever
// fetched and cached (ticket 04's opened marker). false = never
// opened: the caller shows a loading state and fetches. true with an
// empty comment cache = the discussion is genuinely empty.
func (s *Store) DetailFetched(ctx context.Context, itemID string) (bool, error) {
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM detail_fetch WHERE item_id = ?`, itemID).Scan(new(int))
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("probe detail_fetch %s: %w", itemID, err)
	}
	return true, nil
}

// CommentsForItem returns one item's cached comment tree in tree
// order (the order SaveComments wrote: floors in payload order, each
// followed by its subtree, parents strictly before children) — nil
// when the item was never opened. A parent link always refers to an
// earlier row, so the UI can rebuild or flatten the forest in one
// forward pass.
func (s *Store) CommentsForItem(ctx context.Context, itemID string) ([]core.Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, parent_id, author, text, score, created_at, num_children
		FROM comments WHERE item_id = ? ORDER BY seq, rowid`, itemID)
	if err != nil {
		return nil, fmt.Errorf("load comments %s: %w", itemID, err)
	}
	defer rows.Close()
	var out []core.Comment
	for rows.Next() {
		var cm core.Comment
		var parent sql.NullString
		var score sql.NullInt64
		var created sql.NullTime
		var kids sql.NullInt64
		if err := rows.Scan(&cm.ID, &parent, &cm.Author, &cm.Text, &score, &created, &kids); err != nil {
			return nil, err
		}
		if parent.Valid {
			cm.ParentID = parent.String
		}
		cm.Score = nullInt(score)
		if kids.Valid {
			cm.NumChildren = nullInt(kids)
		}
		if created.Valid {
			cm.CreatedAt = &created.Time
		}
		out = append(out, cm)
	}
	return out, rows.Err()
}

// SaveQuestionInfo caches one question's meta triple onto its
// detail_fetch row (ticket 06) — the question layer's meta line
// (提问人 提问 · N 个回答 · N 万浏览) renders from it on offline
// re-opens. An upsert, so it also works standalone (the row then
// opens with fetched_at now); normally SaveComments has just landed
// the row in the same refresh.
func (s *Store) SaveQuestionInfo(ctx context.Context, itemID string, q core.QuestionInfo) error {
	blob, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("encode question info %s: %w", itemID, err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO detail_fetch (item_id, fetched_at, question)
		VALUES (?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET question = excluded.question`,
		itemID, time.Now().UTC(), string(blob)); err != nil {
		return fmt.Errorf("save question info %s: %w", itemID, err)
	}
	return nil
}

// QuestionInfoForItem reads the cached meta triple back: ok=false
// (zero value, no error) when the item was never opened or carries
// no triple — hn stories never do. A corrupt blob reads as absent
// too: the meta line degrades away rather than failing the open.
func (s *Store) QuestionInfoForItem(ctx context.Context, itemID string) (core.QuestionInfo, bool, error) {
	var blob string
	err := s.db.QueryRowContext(ctx,
		`SELECT question FROM detail_fetch WHERE item_id = ?`, itemID).Scan(&blob)
	if err == sql.ErrNoRows || blob == "" {
		return core.QuestionInfo{}, false, nil
	}
	if err != nil {
		return core.QuestionInfo{}, false, fmt.Errorf("probe question info %s: %w", itemID, err)
	}
	var q core.QuestionInfo
	if err := json.Unmarshal([]byte(blob), &q); err != nil {
		return core.QuestionInfo{}, false, nil
	}
	return q, true, nil
}
