package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/store"
)

// ErrNoSession means there was no token, or it no longer maps to a live
// session. Both are treated as anonymous.
var ErrNoSession = errors.New("no live session")

// Manager creates, validates and revokes sessions.
type Manager struct {
	Store       *store.Store
	Now         func() time.Time
	SessionTTL  time.Duration // idle timeout
	MaxLifetime time.Duration // absolute cap from creation
	TouchEvery  time.Duration
	LockAfter   int
	LockFor     time.Duration
}

func NewManager(s *store.Store) *Manager {
	return &Manager{
		Store:       s,
		Now:         time.Now,
		SessionTTL:  12 * time.Hour,
		MaxLifetime: 30 * 24 * time.Hour,
		TouchEvery:  5 * time.Minute,
		LockAfter:   5,
		LockFor:     15 * time.Minute,
	}
}

// SignIn records a successful login and returns the cookie token and its
// expiry. The session row and its audit entry commit together.
func (m *Manager) SignIn(ctx context.Context, tenantID uuid.UUID, user store.User, ip, userAgent string) (string, time.Time, error) {
	token, hash, err := NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := m.Now().Add(m.SessionTTL)

	err = m.Store.InTenant(ctx, tenantID, func(q *store.Store) error {
		if _, err := q.CreateSession(ctx, store.CreateSessionParams{
			TenantID:  tenantID,
			UserID:    user.ID,
			TokenHash: hash,
			IP:        ip,
			UserAgent: userAgent,
			ExpiresAt: expires,
		}); err != nil {
			return err
		}
		if err := q.RegisterLogin(ctx, tenantID, user.ID); err != nil {
			return err
		}
		return audit.Write(ctx, q, tenantID, &user, ip, userAgent, audit.Entry{
			Action:   "auth.login",
			Entity:   "user",
			EntityID: &user.ID,
		})
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign in: %w", err)
	}
	return token, expires, nil
}

// Authenticate resolves a cookie token to a user and their permissions, and
// slides the idle deadline forward at most once per TouchEvery.
func (m *Manager) Authenticate(ctx context.Context, tenantID uuid.UUID, token string) (SignedIn, error) {
	if token == "" {
		return SignedIn{}, ErrNoSession
	}

	var out SignedIn
	err := m.Store.InTenant(ctx, tenantID, func(q *store.Store) error {
		si, err := q.SignedInByToken(ctx, tenantID, HashToken(token))
		if err != nil {
			return err
		}
		perms, err := q.PermissionsFor(ctx, tenantID, si.User.ID, si.User.Role)
		if err != nil {
			return err
		}
		out = SignedIn{Session: si.Session, User: si.User, Permissions: perms}

		if m.Now().Sub(si.Session.LastSeenAt) >= m.TouchEvery {
			expires := m.Now().Add(m.SessionTTL)
			if cap := si.Session.CreatedAt.Add(m.MaxLifetime); expires.After(cap) {
				expires = cap
			}
			if err := q.TouchSession(ctx, tenantID, si.Session.ID, expires); err != nil {
				return err
			}
			out.Session.ExpiresAt = expires
		}
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		return SignedIn{}, ErrNoSession
	}
	if err != nil {
		return SignedIn{}, fmt.Errorf("authenticate: %w", err)
	}
	return out, nil
}

// SignOut revokes one session and records who ended it.
func (m *Manager) SignOut(ctx context.Context, tenantID uuid.UUID, user store.User, sessionID uuid.UUID, ip, userAgent string) error {
	err := m.Store.InTenant(ctx, tenantID, func(q *store.Store) error {
		if err := q.RevokeSession(ctx, tenantID, sessionID); err != nil {
			return err
		}
		return audit.Write(ctx, q, tenantID, &user, ip, userAgent, audit.Entry{
			Action:   "auth.logout",
			Entity:   "user",
			EntityID: &user.ID,
		})
	})
	if err != nil {
		return fmt.Errorf("sign out: %w", err)
	}
	return nil
}
