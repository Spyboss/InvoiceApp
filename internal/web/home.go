package web

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) homePage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.User(r.Context())
		t, hasTenant := tenant.FromContext(r.Context())
		if !ok || !hasTenant {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		permissions := make([]string, 0, len(u.Permissions))
		for p := range u.Permissions {
			permissions = append(permissions, p)
		}
		sort.Strings(permissions)

		s.render(w, r, http.StatusOK, views.Home(views.HomeData{
			TenantName:  t.Name,
			UserName:    u.User.Name,
			Email:       u.User.Email,
			Role:        u.User.Role,
			Permissions: permissions,
			CSRFToken:   csrfToken(r.Context()),
			CanSeeAudit: auth.Can(r.Context(), "audit.view"),
			SessionEnds: u.Session.ExpiresAt,
		}))
	}
}

func (s *Server) auditPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.User(r.Context())
		t, hasTenant := tenant.FromContext(r.Context())
		if !ok || !hasTenant {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var records []store.AuditRecord
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			records, err = q.ListAudit(r.Context(), t.ID, 100)
			return err
		})
		if err != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "audit list failed",
				slog.Any("error", err),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
			s.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
				"The audit log could not be loaded.")
			return
		}

		rows := make([]views.AuditRow, 0, len(records))
		for _, rec := range records {
			rows = append(rows, views.AuditRow{
				Actor:  deref(rec.ActorName),
				Action: rec.Action,
				Entity: rec.Entity,
				IP:     rec.IP,
				At:     rec.CreatedAt,
			})
		}

		s.render(w, r, http.StatusOK, views.Audit(views.AuditData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			CSRFToken:  csrfToken(r.Context()),
			Rows:       rows,
		}))
	}
}

func deref(p *string) string {
	if p == nil {
		return "system"
	}
	return *p
}
