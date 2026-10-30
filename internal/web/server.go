// Package web wires routes to handlers. It owns html rendering and nothing
// else: domain rules live in their own packages.
package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// Server carries the dependencies every handler needs.
type Server struct {
	log  *slog.Logger
	ping func(context.Context) error
}

// New returns the http handler for the whole site, middleware included.
func New(log *slog.Logger, pool *pgxpool.Pool) http.Handler {
	return newServer(log, pool.Ping)
}

func newServer(log *slog.Logger, ping func(context.Context) error) http.Handler {
	s := &Server{log: log, ping: ping}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", s.healthz())
	mux.Handle("GET /static/", staticHandler())
	mux.Handle("/", s.notFound())

	return httpx.Chain(mux,
		httpx.WithRequestID(),
		httpx.RealIP(),
		httpx.Logger(log),
		httpx.Recoverer(log),
		httpx.SecureHeaders(),
	)
}

func (s *Server) notFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusNotFound, views.ErrorPage(
			"Page not found",
			"There is nothing at this address.",
			httpx.RequestID(r.Context()),
		))
	}
}
