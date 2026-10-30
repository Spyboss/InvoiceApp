package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// PermissionsFor returns the effective permission set: everything the role
// grants, plus explicit grants, minus explicit denials. Must run inside
// InTenant.
func (s *Store) PermissionsFor(ctx context.Context, tenantID, userID uuid.UUID, role string) (map[string]bool, error) {
	const q = `
		with grants as (
		    select permission from role_permissions where role = $1
		    union
		    select permission from user_permissions
		    where tenant_id = $2 and user_id = $3 and granted
		),
		denies as (
		    select permission from user_permissions
		    where tenant_id = $2 and user_id = $3 and not granted
		)
		select permission from grants
		except
		select permission from denies`

	rows, err := s.db.Query(ctx, q, role, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("load permissions: %w", err)
	}
	defer rows.Close()

	perms := make(map[string]bool)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms[p] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load permissions: %w", err)
	}
	return perms, nil
}
