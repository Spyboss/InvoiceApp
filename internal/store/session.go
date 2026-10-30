package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Session is one signed in browser. Only the hash of the cookie value is
// stored, so a database leak does not hand over live sessions.
type Session struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// SignedIn is a session joined with the user it belongs to.
type SignedIn struct {
	Session Session
	User    User
}

type CreateSessionParams struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	TokenHash []byte
	IP        string
	UserAgent string
	ExpiresAt time.Time
}

// CreateSession must run inside InTenant.
func (s *Store) CreateSession(ctx context.Context, p CreateSessionParams) (Session, error) {
	const q = `
		insert into sessions (tenant_id, user_id, token_hash, ip, user_agent, expires_at)
		values ($1, $2, $3, nullif($4, '')::inet, nullif($5, ''), $6)
		returning id, tenant_id, user_id, created_at, expires_at`

	var sess Session
	err := s.db.QueryRow(ctx, q, p.TenantID, p.UserID, p.TokenHash, p.IP, p.UserAgent, p.ExpiresAt).
		Scan(&sess.ID, &sess.TenantID, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

// SignedInByToken returns the session and user for a live token. Must run
// inside InTenant.
func (s *Store) SignedInByToken(ctx context.Context, tenantID uuid.UUID, tokenHash []byte) (SignedIn, error) {
	q := `
		select s.id, s.tenant_id, s.user_id, s.created_at, s.last_seen_at, s.expires_at,
		       u.id, u.tenant_id, u.email, u.name, u.password_hash, u.role,
		       u.is_active, u.failed_attempts, u.locked_until, u.last_login_at, u.created_at
		from sessions s
		join users u on u.id = s.user_id
		where s.tenant_id = $1
		  and s.token_hash = $2
		  and s.revoked_at is null
		  and s.expires_at > now()
		  and u.is_active
		  and u.deleted_at is null`

	var out SignedIn
	err := s.db.QueryRow(ctx, q, tenantID, tokenHash).Scan(
		&out.Session.ID, &out.Session.TenantID, &out.Session.UserID, &out.Session.CreatedAt,
		&out.Session.LastSeenAt, &out.Session.ExpiresAt,
		&out.User.ID, &out.User.TenantID, &out.User.Email, &out.User.Name, &out.User.PasswordHash, &out.User.Role,
		&out.User.IsActive, &out.User.FailedAttempts, &out.User.LockedUntil, &out.User.LastLoginAt, &out.User.CreatedAt,
	)
	if err != nil {
		return SignedIn{}, fmt.Errorf("load session: %w", notFound(err))
	}
	return out, nil
}

// TouchSession records activity and pushes the idle deadline out. Must run
// inside InTenant.
func (s *Store) TouchSession(ctx context.Context, tenantID, sessionID uuid.UUID, expiresAt time.Time) error {
	const q = `
		update sessions
		set last_seen_at = now(), expires_at = $3
		where tenant_id = $1 and id = $2 and revoked_at is null`
	if _, err := s.db.Exec(ctx, q, tenantID, sessionID, expiresAt); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// RevokeSession must run inside InTenant.
func (s *Store) RevokeSession(ctx context.Context, tenantID, sessionID uuid.UUID) error {
	const q = `update sessions set revoked_at = now() where tenant_id = $1 and id = $2 and revoked_at is null`
	if _, err := s.db.Exec(ctx, q, tenantID, sessionID); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
