package store

import (
	"context"
	"fmt"
)

// CountTenants is used once, during first install, to decide whether a
// hostname may bootstrap a new dealership.
func (s *Store) CountTenants(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `select count(*) from tenants`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count tenants: %w", err)
	}
	return n, nil
}
