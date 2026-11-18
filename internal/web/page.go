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

// navLinks builds the sidebar for whoever is signed in. A link only appears if
// the user can open it, so nobody is offered a page that answers 403. Groups
// follow the order a day is worked (D31, docs/COUNTER.md); a group whose links
// are all filtered out is absent from the slice, so it never renders empty.
func navLinks(ctx context.Context, path string) []views.NavLink {
	candidates := []struct {
		href       string
		label      string
		group      string
		permission string
	}{
		// counter: what a sale needs, then the money, then the drawer.
		{"/documents", "Documents", "counter", "document.read"},
		{"/customers", "Customers", "counter", "customer.read"},
		{"/vehicles", "Vehicles", "counter", "vehicle.read"},
		{"/stock", "Stock", "counter", "stock.read"},
		{"/payments", "Payments", "counter", "payment.read"},
		{"/day-end", "Day end", "counter", "dayend.read"},
		// oversight: what the owner and the manager look at.
		{"/reports", "Reports", "oversight", "report.view"},
		{"/alerts", "Alerts", "oversight", "alert.view"},
		{"/reconciliation", "Reconciliation", "oversight", "reconcile.run"},
		// admin: opened to change something, not to read the day.
		{"/import", "Import", "admin", "import.run"},
		{"/audit", "Audit", "admin", "audit.view"},
		{"/settings", "Settings", "admin", "settings.write"},
		{"/users", "Users", "admin", "user.manage"},
	}

	links := make([]views.NavLink, 0, len(candidates))
	for _, c := range candidates {
		if !auth.Can(ctx, c.permission) {
			continue
		}
		links = append(links, views.NavLink{
			Href:  c.href,
			Label: c.label,
			Group: c.group,
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
