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

// The roles a person can hold. The database allows exactly these five.
const (
	RoleOwner    = "OWNER"
	RoleManager  = "MANAGER"
	RoleSales    = "SALES"
	RoleAccounts = "ACCOUNTS"
	RoleService  = "SERVICE"
)

// Roles is the order the screen offers them in: the people who decide, then
// the people who do the work.
var Roles = []string{RoleOwner, RoleManager, RoleSales, RoleAccounts, RoleService}

// ValidRole reports whether the role is one this app knows.
func ValidRole(role string) bool {
	switch role {
	case RoleOwner, RoleManager, RoleSales, RoleAccounts, RoleService:
		return true
	}
	return false
}

// RoleLabel is the words a person reads, not the code in the database.
func RoleLabel(role string) string {
	switch role {
	case RoleOwner:
		return "Owner"
	case RoleManager:
		return "Manager"
	case RoleSales:
		return "Sales"
	case RoleAccounts:
		return "Accounts"
	case RoleService:
		return "Service"
	}
	return role
}

// ListUsers returns everybody who can sign in, active or not, so the owner can
// see who was switched off and why they cannot log in. Must run inside
// InTenant.
func (s *Store) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]User, error) {
	q := `select ` + userColumns + `
		from users
		where tenant_id = $1 and deleted_at is null
		order by is_active desc, role, name`
	rows, err := s.db.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	out := make([]User, 0, 8)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// UpdateUserRole moves a person to another role. The database refuses to leave
// a dealership without an active owner. Must run inside InTenant.
func (s *Store) UpdateUserRole(ctx context.Context, tenantID, userID uuid.UUID, role string) (User, error) {
	if !ValidRole(role) {
		return User{}, ErrInvalidUser
	}
	return s.updateUser(ctx, tenantID, userID, `role = $3`, role)
}

// SetUserActive switches a person on or off. A switched off person keeps their
// rows: the documents they issued, the receipts they took and the audit trail
// are history and do not move. Must run inside InTenant.
func (s *Store) SetUserActive(ctx context.Context, tenantID, userID uuid.UUID, active bool) (User, error) {
	return s.updateUser(ctx, tenantID, userID, `is_active = $3`, active)
}

func (s *Store) updateUser(ctx context.Context, tenantID, userID uuid.UUID, set string, arg any) (User, error) {
	q := `update users set ` + set + `, updated_at = now()
		where tenant_id = $1 and id = $2 and deleted_at is null
		returning ` + userColumns

	u, err := scanUser(s.db.QueryRow(ctx, q, tenantID, userID, arg))
	if err == nil {
		return u, nil
	}
	if sqlState(err) == "USR01" {
		return User{}, ErrLastOwner
	}
	return User{}, err
}

// SetPasswordHash replaces a password. The caller hashes it; the store never
// sees a plain one. Must run inside InTenant.
func (s *Store) SetPasswordHash(ctx context.Context, tenantID, userID uuid.UUID, hash string) error {
	const q = `update users set password_hash = $3, failed_attempts = 0, locked_until = null,
		updated_at = now() where tenant_id = $1 and id = $2 and deleted_at is null`
	tag, err := s.db.Exec(ctx, q, tenantID, userID, hash)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeSessionsFor signs a person out everywhere. Called when a password is
// reset or an account is switched off, so the old password cannot still be in
// use on a device somebody else is holding. Must run inside InTenant.
func (s *Store) RevokeSessionsFor(ctx context.Context, tenantID, userID uuid.UUID) error {
	const q = `update sessions set revoked_at = now()
		where tenant_id = $1 and user_id = $2 and revoked_at is null`
	if _, err := s.db.Exec(ctx, q, tenantID, userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

// PermissionOverridesFor returns only the per-user grants and denials, which
// is what the screen edits: everything else comes from the role.
func (s *Store) PermissionOverridesFor(ctx context.Context, tenantID, userID uuid.UUID) (map[string]bool, error) {
	const q = `select permission, granted from user_permissions
		where tenant_id = $1 and user_id = $2`
	rows, err := s.db.Query(ctx, q, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("load permission overrides: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var permission string
		var granted bool
		if err := rows.Scan(&permission, &granted); err != nil {
			return nil, fmt.Errorf("scan permission override: %w", err)
		}
		out[permission] = granted
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load permission overrides: %w", err)
	}
	return out, nil
}

// SetPermissionOverride grants or denies one permission to one person, or
// removes the override when granted is nil so the role speaks again. Must run
// inside InTenant.
func (s *Store) SetPermissionOverride(ctx context.Context, tenantID, userID uuid.UUID, permission string, granted *bool) error {
	if granted == nil {
		const q = `delete from user_permissions where tenant_id = $1 and user_id = $2 and permission = $3`
		if _, err := s.db.Exec(ctx, q, tenantID, userID, permission); err != nil {
			return fmt.Errorf("clear permission override: %w", err)
		}
		return nil
	}

	const q = `
		insert into user_permissions (tenant_id, user_id, permission, granted)
		values ($1, $2, $3, $4)
		on conflict (user_id, permission) do update set granted = excluded.granted`
	if _, err := s.db.Exec(ctx, q, tenantID, userID, permission, *granted); err != nil {
		return fmt.Errorf("set permission override: %w", err)
	}
	return nil
}

// AllPermissions lists every permission this app knows, taken from the role
// table so the screen and the middleware cannot drift apart.
func (s *Store) AllPermissions(ctx context.Context) ([]string, error) {
	const q = `select distinct permission from role_permissions order by permission`
	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, 32)
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		out = append(out, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	return out, nil
}

// RolePermissions returns what a role grants, so the screen can show where a
// person's access comes from.
func (s *Store) RolePermissions(ctx context.Context, role string) (map[string]bool, error) {
	const q = `select permission from role_permissions where role = $1`
	rows, err := s.db.Query(ctx, q, role)
	if err != nil {
		return nil, fmt.Errorf("load role permissions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("scan role permission: %w", err)
		}
		out[permission] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load role permissions: %w", err)
	}
	return out, nil
}
