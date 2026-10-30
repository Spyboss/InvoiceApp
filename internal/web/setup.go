package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

var errSetupClosed = errors.New("setup already complete")

// setupPage is the first run screen. With no tenant for the host it also
// creates the dealership, but only while the installation has no tenants at
// all, so a public hostname cannot be used to spin up accounts.
func (s *Server) setupPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, hasTenant := tenant.FromContext(r.Context())
		csrf := csrfToken(r.Context())

		if !hasTenant {
			count, err := s.store.CountTenants(r.Context())
			if err != nil {
				s.setupFailed(w, r, err)
				return
			}
			if count > 0 {
				s.renderError(w, r, http.StatusNotFound, "Setup is closed",
					"This installation already has an account. Sign in instead.")
				return
			}
			s.render(w, r, http.StatusOK, views.Setup(true, "", "", "", "", "", csrf))
			return
		}

		if _, signedIn := auth.User(r.Context()); signedIn {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		users, err := s.countUsers(r, t.ID)
		if err != nil {
			s.setupFailed(w, r, err)
			return
		}
		if users > 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		s.render(w, r, http.StatusOK, views.Setup(false, t.Name, t.Name, "", "", "", csrf))
	}
}

func (s *Server) setupSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, hasTenant := tenant.FromContext(r.Context())
		host := tenant.NormaliseHost(r.Host)

		dealership := strings.TrimSpace(r.PostFormValue("dealership"))
		name := strings.TrimSpace(r.PostFormValue("name"))
		email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
		password := r.PostFormValue("password")
		confirm := r.PostFormValue("confirm")

		fail := func(status int, message string) {
			title := ""
			if hasTenant {
				title = t.Name
			}
			s.render(w, r, status, views.Setup(!hasTenant, title, dealership, name, email, message, csrfToken(r.Context())))
		}

		if !hasTenant {
			count, err := s.store.CountTenants(r.Context())
			if err != nil {
				s.setupFailed(w, r, err)
				return
			}
			if count > 0 {
				s.renderError(w, r, http.StatusNotFound, "Setup is closed",
					"This installation already has an account. Sign in instead.")
				return
			}
			if dealership == "" {
				fail(http.StatusBadRequest, "Enter the dealership name.")
				return
			}
		}

		if name == "" {
			fail(http.StatusBadRequest, "Enter your name.")
			return
		}
		if _, err := mail.ParseAddress(email); err != nil {
			fail(http.StatusBadRequest, "Enter a valid email address.")
			return
		}
		if err := auth.ValidatePassword(password); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		if password != confirm {
			fail(http.StatusBadRequest, "The two passwords do not match.")
			return
		}

		tenantID := t.ID
		if !hasTenant {
			slug := slugify(dealership)
			created, err := s.store.CreateTenant(r.Context(), slug, dealership, host)
			if err != nil {
				s.log.LogAttrs(r.Context(), slog.LevelWarn, "tenant create failed",
					slog.Any("error", err),
					slog.String("host", host),
				)
				fail(http.StatusBadRequest, "That dealership name or address is already in use.")
				return
			}
			tenantID = created.ID
		}

		hash, err := auth.HashPassword(password)
		if err != nil {
			s.setupFailed(w, r, err)
			return
		}

		var owner store.User
		err = s.store.InTenant(r.Context(), tenantID, func(q *store.Store) error {
			if err := q.LockTenant(r.Context(), tenantID); err != nil {
				return err
			}
			users, err := q.CountUsers(r.Context(), tenantID)
			if err != nil {
				return err
			}
			if users > 0 {
				return errSetupClosed
			}

			created, err := q.CreateUser(r.Context(), store.CreateUserParams{
				TenantID:     tenantID,
				Email:        email,
				Name:         name,
				PasswordHash: hash,
				Role:         "OWNER",
			})
			if err != nil {
				return err
			}
			owner = created

			if !hasTenant {
				if err := audit.Write(r.Context(), q, tenantID, &owner, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
					Action:   "tenant.create",
					Entity:   "tenant",
					EntityID: &tenantID,
					After:    map[string]string{"name": dealership, "domain": host},
				}); err != nil {
					return err
				}
			}
			return audit.Write(r.Context(), q, tenantID, &owner, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "user.create",
				Entity:   "user",
				EntityID: &owner.ID,
				After:    map[string]string{"email": email, "name": name, "role": "OWNER"},
			})
		})
		if errors.Is(err, errSetupClosed) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if err != nil {
			s.setupFailed(w, r, err)
			return
		}

		if s.forget != nil {
			s.forget(host)
		}

		token, expires, err := s.auth.SignIn(r.Context(), tenantID, owner, httpx.ClientIP(r.Context()), r.UserAgent())
		if err != nil {
			s.setupFailed(w, r, err)
			return
		}
		s.setSessionCookie(w, token, expires)
		s.rotateCSRF(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) countUsers(r *http.Request, tenantID uuid.UUID) (int, error) {
	var n int
	err := s.store.InTenant(r.Context(), tenantID, func(q *store.Store) error {
		var err error
		n, err = q.CountUsers(r.Context(), tenantID)
		return err
	})
	return n, err
}

func (s *Server) setupFailed(w http.ResponseWriter, r *http.Request, err error) {
	s.log.LogAttrs(r.Context(), slog.LevelError, "setup failed",
		slog.Any("error", err),
		slog.String("request_id", httpx.RequestID(r.Context())),
	)
	s.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
		"The account could not be created. The error has been logged.")
}

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	if out == "" {
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		return "dealer-" + hex.EncodeToString(buf)
	}
	return out
}
