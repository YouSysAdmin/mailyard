// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"context"
	"time"
)

// PurgeDeliveriesOlderThan trims the delivery log. Platform
// maintenance, so unscoped by project.
func (s *Store) PurgeDeliveriesOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM webhook_deliveries WHERE created_at < ?`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
