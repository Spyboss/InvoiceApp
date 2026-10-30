package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// AuditEntry is one line in the append only trail. Before and After are
// already marshalled json, or nil when the action has no payload.
type AuditEntry struct {
	TenantID  uuid.UUID
	ActorID   *uuid.UUID
	Action    string
	Entity    string
	EntityID  *uuid.UUID
	Before    *string
	After     *string
	IP        string
	UserAgent string
}

// InsertAudit must run in the same transaction as the write it describes.
func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	const q = `
		insert into audit_log (tenant_id, actor_user_id, action, entity, entity_id, before, after, ip, user_agent)
		values ($1, $2, $3, $4, $5, nullif($6, '')::jsonb, nullif($7, '')::jsonb, nullif($8, '')::inet, nullif($9, ''))`
	if _, err := s.db.Exec(ctx, q, e.TenantID, e.ActorID, e.Action, e.Entity, e.EntityID,
		e.Before, e.After, e.IP, e.UserAgent); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}
