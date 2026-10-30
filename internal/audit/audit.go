// Package audit writes the append only trail. Every write in the app calls
// this inside the same transaction as the change it describes.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// Entry describes what happened. Before and After may be nil.
type Entry struct {
	Action   string
	Entity   string
	EntityID *uuid.UUID
	Before   any
	After    any
}

// Write records one entry. db must be a transaction bound store so the audit
// row and the change commit or roll back together.
func Write(ctx context.Context, db *store.Store, tenantID uuid.UUID, actor *store.User, ip, userAgent string, e Entry) error {
	before, err := marshal(e.Before)
	if err != nil {
		return fmt.Errorf("audit before: %w", err)
	}
	after, err := marshal(e.After)
	if err != nil {
		return fmt.Errorf("audit after: %w", err)
	}

	var actorID *uuid.UUID
	if actor != nil {
		id := actor.ID
		actorID = &id
	}

	if err := db.InsertAudit(ctx, store.AuditEntry{
		TenantID:  tenantID,
		ActorID:   actorID,
		Action:    e.Action,
		Entity:    e.Entity,
		EntityID:  e.EntityID,
		Before:    before,
		After:     after,
		IP:        ip,
		UserAgent: userAgent,
	}); err != nil {
		return err
	}
	return nil
}

func marshal(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}
