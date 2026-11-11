// comments.go: the comment cache's persistence (ticket 03). One item
// owns one comment tree, written whole (delete + insert in a single
// transaction) every time a Detail fetch comes back — repeat opens
// and background refreshes replace, never append, so no duplicate
// rows and no stale floors can survive. Tree order (parents strictly
// before children, DFS pre-order from the fetch) is preserved as
// insertion order; reads return it via rowid, which a whole-tree
// replace renumbers coherently.

package store

import (
	"context"
	"database/sql"
	"fmt"

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
	for _, cm := range comments {
		var parent any // NULL for a top-level floor
		if cm.ParentID != "" {
			parent = cm.ParentID
		}
		var score any
		if cm.Score != nil {
			score = *cm.Score
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO comments
			(item_id, id, parent_id, author, text, score, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			itemID, cm.ID, parent, cm.Author, cm.Text, score, cm.CreatedAt); err != nil {
			return fmt.Errorf("store comment %s/%s: %w", itemID, cm.ID, err)
		}
	}
	return tx.Commit()
}

// CommentsForItem returns one item's cached comment tree in tree
// order (the order SaveComments wrote: floors in payload order, each
// followed by its subtree, parents strictly before children) — nil
// when the item was never opened. A parent link always refers to an
// earlier row, so the UI can rebuild or flatten the forest in one
// forward pass.
func (s *Store) CommentsForItem(ctx context.Context, itemID string) ([]core.Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, parent_id, author, text, score, created_at
		FROM comments WHERE item_id = ? ORDER BY rowid`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Comment
	for rows.Next() {
		var cm core.Comment
		var parent sql.NullString
		var score sql.NullInt64
		var created sql.NullTime
		if err := rows.Scan(&cm.ID, &parent, &cm.Author, &cm.Text, &score, &created); err != nil {
			return nil, err
		}
		if parent.Valid {
			cm.ParentID = parent.String
		}
		cm.Score = nullInt(score)
		if created.Valid {
			cm.CreatedAt = &created.Time
		}
		out = append(out, cm)
	}
	return out, rows.Err()
}
