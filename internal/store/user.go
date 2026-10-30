package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// User is a person who can sign in to one tenant.
type User struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Email          string
	Name           string
	PasswordHash   string
	Role           string
	IsActive       bool
	FailedAttempts int32
	LockedUntil    *time.Time
	LastLoginAt    *time.Time
	CreatedAt      time.Time
}

const userColumns = `id, tenant_id, email, name, password_hash, role, is_active,
	failed_attempts, locked_until, last_login_at, created_at`

// UserByEmail must run inside InTenant.
func (s *Store) UserByEmail(ctx context.Context, tenantID uuid.UUID, email string) (User, error) {
	q := `select ` + userColumns + ` from users where tenant_id = $1 and email = $2 and deleted_at is null`
	return scanUser(s.db.QueryRow(ctx, q, tenantID, email))
}

// UserByID must run inside InTenant.
func (s *Store) UserByID(ctx context.Context, tenantID, id uuid.UUID) (User, error) {
	q := `select ` + userColumns + ` from users where tenant_id = $1 and id = $2 and deleted_at is null`
	return scanUser(s.db.QueryRow(ctx, q, tenantID, id))
}

type CreateUserParams struct {
	TenantID     uuid.UUID
	Email        string
	Name         string
	PasswordHash string
	Role         string
}

// CreateUser must run inside InTenant.
func (s *Store) CreateUser(ctx context.Context, p CreateUserParams) (User, error) {
	q := `
		insert into users (tenant_id, email, name, password_hash, role)
		values ($1, $2, $3, $4, $5)
		returning ` + userColumns
	return scanUser(s.db.QueryRow(ctx, q, p.TenantID, p.Email, p.Name, p.PasswordHash, p.Role))
}

// CountUsers must run inside InTenant.
func (s *Store) CountUsers(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `select count(*) from users where tenant_id = $1 and deleted_at is null`, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// RegisterFailedLogin increments the counter and locks the account once the
// threshold is reached. Must run inside InTenant.
func (s *Store) RegisterFailedLogin(ctx context.Context, tenantID, userID uuid.UUID, lockAfter int, lockFor time.Duration) error {
	const q = `
		update users
		set failed_attempts = failed_attempts + 1,
		    locked_until = case
		        when failed_attempts + 1 >= $3 then now() + $4::interval
		        else locked_until
		    end,
		    updated_at = now()
		where tenant_id = $1 and id = $2`
	interval := fmt.Sprintf("%d seconds", int(lockFor.Seconds()))
	if _, err := s.db.Exec(ctx, q, tenantID, userID, lockAfter, interval); err != nil {
		return fmt.Errorf("register failed login: %w", err)
	}
	return nil
}

// RegisterLogin clears the failure counter after a successful sign in. Must
// run inside InTenant.
func (s *Store) RegisterLogin(ctx context.Context, tenantID, userID uuid.UUID) error {
	const q = `
		update users
		set failed_attempts = 0, locked_until = null, last_login_at = now(), updated_at = now()
		where tenant_id = $1 and id = $2`
	if _, err := s.db.Exec(ctx, q, tenantID, userID); err != nil {
		return fmt.Errorf("register login: %w", err)
	}
	return nil
}

func scanUser(row rowScanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.PasswordHash, &u.Role,
		&u.IsActive, &u.FailedAttempts, &u.LockedUntil, &u.LastLoginAt, &u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", notFound(err))
	}
	return u, nil
}
