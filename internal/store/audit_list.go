package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AuditRecord is one row of the trail, joined with the actor's name.
type AuditRecord struct {
	ID        uuid.UUID
	ActorName *string
	Action    string
	Entity    string
	EntityID  *uuid.UUID
	IP        string
	CreatedAt time.Time
}

// ListAudit must run inside InTenant.
func (s *Store) ListAudit(ctx context.Context, tenantID uuid.UUID, limit int) ([]AuditRecord, error) {
	const q = `
		select a.id, u.name, a.action, a.entity, a.entity_id, coalesce(host(a.ip), ''), a.created_at
		from audit_log a
		left join users u on u.id = a.actor_user_id
		where a.tenant_id = $1
		order by a.created_at desc, a.id desc
		limit $2`

	rows, err := s.db.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var out []AuditRecord
	for rows.Next() {
		var r AuditRecord
		if err := rows.Scan(&r.ID, &r.ActorName, &r.Action, &r.Entity, &r.EntityID, &r.IP, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	return out, nil
}
