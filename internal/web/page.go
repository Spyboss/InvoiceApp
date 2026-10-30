package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// pageUser is the tenant of the request host plus the signed in user.
func (s *Server) pageUser(r *http.Request) (store.Tenant, auth.SignedIn, bool) {
	t, hasTenant := tenant.FromContext(r.Context())
	u, signedIn := auth.User(r.Context())
	if !hasTenant || !signedIn {
		return store.Tenant{}, auth.SignedIn{}, false
	}
	return t, u, true
}

// navLinks builds the header for whoever is signed in. A link only appears if
// the user can open it, so nobody is offered a page that answers 403.
func navLinks(ctx context.Context, path string) []views.NavLink {
	candidates := []struct {
		href       string
		label      string
		permission string
	}{
		{"/customers", "Customers", "customer.read"},
		{"/vehicles", "Vehicles", "vehicle.read"},
		{"/stock", "Stock", "stock.read"},
		{"/documents", "Documents", "document.read"},
		{"/payments", "Payments", "payment.read"},
		{"/day-end", "Day end", "dayend.read"},
		{"/reports", "Reports", "report.view"},
		{"/audit", "Audit", "audit.view"},
	}

	links := make([]views.NavLink, 0, len(candidates))
	for _, c := range candidates {
		if !auth.Can(ctx, c.permission) {
			continue
		}
		links = append(links, views.NavLink{
			Href:  c.href,
			Label: c.label,
			Here:  path == c.href || strings.HasPrefix(path, c.href+"/"),
		})
	}
	return links
}

// pathID parses the {id} segment. A malformed uuid is a bad address, not a
// server fault, so the caller answers 404.
func pathID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	return id, err == nil
}

// internalError logs the cause and shows the generic page, which carries the
// request id so this log line can be found.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.LogAttrs(r.Context(), slog.LevelError, what+" failed",
		slog.Any("error", err),
		slog.String("request_id", httpx.RequestID(r.Context())),
	)
	s.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
		"The page could not be loaded. The error has been logged.")
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
