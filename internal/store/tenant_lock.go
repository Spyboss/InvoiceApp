package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// LockTenant takes a row lock on the tenant for the rest of the transaction.
// First-run setup uses it so two simultaneous submissions cannot both create
// an owner.
func (s *Store) LockTenant(ctx context.Context, id uuid.UUID) error {
	var got uuid.UUID
	if err := s.db.QueryRow(ctx, `select id from tenants where id = $1 for update`, id).Scan(&got); err != nil {
		return fmt.Errorf("lock tenant: %w", notFound(err))
	}
	return nil
}
