// Package web wires routes to handlers. It owns html rendering and nothing
// else: domain rules live in their own packages.
package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/store"
)

// Deps is everything the router needs. The two tenant functions are plain
// funcs so tests can resolve hosts without a database.
type Deps struct {
	Log           *slog.Logger
	Store         *store.Store
	Ping          func(ctx context.Context) error
	ResolveTenant func(ctx context.Context, host string) (store.Tenant, error)
	ForgetTenant  func(host string)
	SecureCookies bool
	Now           func() time.Time
}

type Server struct {
	log      *slog.Logger
	store    *store.Store
	auth     *auth.Manager
	limiter  *auth.Limiter
	ping     func(ctx context.Context) error
	resolver func(ctx context.Context, host string) (store.Tenant, error)
	forget   func(host string)
	secure   bool
	now      func() time.Time
}

func New(d Deps) http.Handler {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	s := &Server{
		log:      d.Log,
		store:    d.Store,
		auth:     auth.NewManager(d.Store),
		limiter:  auth.NewLimiter(20, 10*time.Minute),
		ping:     d.Ping,
		resolver: d.ResolveTenant,
		forget:   d.ForgetTenant,
		secure:   d.SecureCookies,
		now:      now,
	}
	s.auth.Now = now

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", s.healthz())
	mux.Handle("GET /static/", staticHandler())
	mux.Handle("GET /setup", s.setupPage())
	mux.Handle("POST /setup", s.setupSubmit())
	mux.Handle("GET /login", s.loginPage())
	mux.Handle("POST /login", s.loginSubmit())
	mux.Handle("POST /logout", s.logoutSubmit())
	mux.Handle("GET /{$}", s.requireUser(s.homePage()))
	mux.Handle("GET /audit", s.requirePermission("audit.view", s.auditPage()))

	mux.Handle("GET /customers", s.requirePermission("customer.read", s.customersPage()))
	mux.Handle("GET /customers/new", s.requirePermission("customer.write", s.customerNewPage()))
	mux.Handle("POST /customers", s.requirePermission("customer.write", s.customerCreate()))
	mux.Handle("GET /customers/{id}", s.requirePermission("customer.read", s.customerPage()))
	mux.Handle("GET /customers/{id}/edit", s.requirePermission("customer.write", s.customerEditPage()))
	mux.Handle("POST /customers/{id}", s.requirePermission("customer.write", s.customerUpdate()))
	mux.Handle("POST /customers/{id}/delete", s.requirePermission("customer.write", s.customerDelete()))

	mux.Handle("GET /vehicles", s.requirePermission("vehicle.read", s.vehiclesPage()))
	mux.Handle("GET /vehicles/new", s.requirePermission("vehicle.write", s.vehicleNewPage()))
	mux.Handle("POST /vehicles", s.requirePermission("vehicle.write", s.vehicleCreate()))
	mux.Handle("GET /vehicles/{id}", s.requirePermission("vehicle.read", s.vehiclePage()))
	mux.Handle("GET /vehicles/{id}/edit", s.requirePermission("vehicle.write", s.vehicleEditPage()))
	mux.Handle("POST /vehicles/{id}", s.requirePermission("vehicle.write", s.vehicleUpdate()))
	mux.Handle("POST /vehicles/{id}/status", s.requirePermission("stock.write", s.vehicleStatusSubmit()))

	mux.Handle("GET /stock", s.requirePermission("stock.read", s.stockPage()))

	mux.Handle("/", s.notFound())

	return httpx.Chain(mux,
		httpx.WithRequestID(),
		httpx.RealIP(),
		httpx.Logger(d.Log),
		httpx.Recoverer(d.Log),
		httpx.SecureHeaders(),
		s.tenantHandler,
		s.sessionHandler,
		s.csrfHandler,
	)
}

func (s *Server) notFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.renderError(w, r, http.StatusNotFound, "Page not found", "There is nothing at this address.")
	}
}
