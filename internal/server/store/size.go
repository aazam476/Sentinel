// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SizeBytes is how much space the database's pages take, for the About
// page (§9.11). The write-ahead log isn't counted: it is folded back into
// the database at every checkpoint.
func (s *Store) SizeBytes(ctx context.Context) (int64, error) {
	var size int64
	err := s.Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&size)
	})
	if err != nil {
		return 0, fmt.Errorf("read the database size: %w", err)
	}
	return size, nil
}
