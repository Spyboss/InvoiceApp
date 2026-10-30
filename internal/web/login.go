package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) loginPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := tenant.FromContext(r.Context())
		if !ok {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		if _, signedIn := auth.User(r.Context()); signedIn {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		s.render(w, r, http.StatusOK, views.Login(t.Name, "", "", csrfToken(r.Context())))
	}
}

func (s *Server) loginSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := tenant.FromContext(r.Context())
		if !ok {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}

		email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
		password := r.PostFormValue("password")
		renderFailure := func(message string) {
			s.render(w, r, http.StatusOK, views.Login(t.Name, email, message, csrfToken(r.Context())))
		}

		ip := httpx.ClientIP(r.Context())
		if !s.limiter.Allow("login:" + ip) {
			renderFailure("Too many attempts from this network. Wait ten minutes.")
			return
		}

		var user store.User
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			u, err := q.UserByEmail(r.Context(), t.ID, email)
			if err != nil {
				return err
			}
			user = u
			return nil
		})

		if errors.Is(err, store.ErrNotFound) {
			auth.BurnPasswordTime(password)
			s.recordLoginFailure(r, t.ID, nil, email)
			renderFailure("Email or password is incorrect.")
			return
		}
		if err != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "login lookup failed",
				slog.Any("error", err),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
			s.renderError(w, r, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
			return
		}

		if user.LockedUntil != nil && user.LockedUntil.After(s.now()) {
			s.recordLoginFailure(r, t.ID, &user, email)
			renderFailure("This account is locked for a few minutes. Try again later.")
			return
		}

		valid, verifyErr := auth.VerifyPassword(user.PasswordHash, password)
		if verifyErr != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "password hash unreadable",
				slog.Any("error", verifyErr),
				slog.String("user_id", user.ID.String()),
			)
		}
		if !valid {
			s.recordLoginFailure(r, t.ID, &user, email)
			renderFailure("Email or password is incorrect.")
			return
		}

		token, expires, err := s.auth.SignIn(r.Context(), t.ID, user, ip, r.UserAgent())
		if err != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "sign in failed",
				slog.Any("error", err),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
			s.renderError(w, r, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
			return
		}

		s.limiter.Reset("login:" + ip)
		s.setSessionCookie(w, token, expires)
		s.rotateCSRF(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) logoutSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.User(r.Context())
		t, hasTenant := tenant.FromContext(r.Context())
		if !ok || !hasTenant {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if err := s.auth.SignOut(r.Context(), t.ID, u.User, u.Session.ID, httpx.ClientIP(r.Context()), r.UserAgent()); err != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "sign out failed",
				slog.Any("error", err),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
		}
		s.clearSessionCookie(w)
		s.rotateCSRF(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// recordLoginFailure increments the lockout counter and writes the audit row
// in one transaction. userId is nil when the email does not exist.
func (s *Server) recordLoginFailure(r *http.Request, tenantID uuid.UUID, user *store.User, email string) {
	err := s.store.InTenant(r.Context(), tenantID, func(q *store.Store) error {
		if user != nil {
			if err := q.RegisterFailedLogin(r.Context(), tenantID, user.ID, s.auth.LockAfter, s.auth.LockFor); err != nil {
				return err
			}
		}
		return audit.Write(r.Context(), q, tenantID, user, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
			Action: "auth.login_failed",
			Entity: "user",
			After:  map[string]string{"email": email},
		})
	})
	if err != nil {
		s.log.LogAttrs(r.Context(), slog.LevelError, "record login failure",
			slog.Any("error", err),
			slog.String("request_id", httpx.RequestID(r.Context())),
		)
	}
}

func (s *Server) rotateCSRF(w http.ResponseWriter) {
	token, err := auth.NewCSRFToken()
	if err != nil {
		return
	}
	s.setCSRFCookie(w, token)
}
