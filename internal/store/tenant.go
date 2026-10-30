package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Tenant is one dealership. The tenants table has no row level security
// because a request has to resolve its tenant before any tenant setting
// exists; the table only ever holds public hostnames.
type Tenant struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	Domain    string
	CreatedAt time.Time
}

func (s *Store) TenantByDomain(ctx context.Context, domain string) (Tenant, error) {
	const q = `select id, slug, name, domain, created_at from tenants where domain = $1`
	return scanTenant(s.db.QueryRow(ctx, q, domain))
}

func (s *Store) TenantBySlug(ctx context.Context, slug string) (Tenant, error) {
	const q = `select id, slug, name, domain, created_at from tenants where slug = $1`
	return scanTenant(s.db.QueryRow(ctx, q, slug))
}

func (s *Store) TenantByID(ctx context.Context, id uuid.UUID) (Tenant, error) {
	const q = `select id, slug, name, domain, created_at from tenants where id = $1`
	return scanTenant(s.db.QueryRow(ctx, q, id))
}

func (s *Store) CreateTenant(ctx context.Context, slug, name, domain string) (Tenant, error) {
	const q = `
		insert into tenants (slug, name, domain)
		values ($1, $2, $3)
		returning id, slug, name, domain, created_at`
	return scanTenant(s.db.QueryRow(ctx, q, slug, name, domain))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTenant(row rowScanner) (Tenant, error) {
	var t Tenant
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Domain, &t.CreatedAt); err != nil {
		return Tenant{}, fmt.Errorf("scan tenant: %w", notFound(err))
	}
	return t, nil
}
