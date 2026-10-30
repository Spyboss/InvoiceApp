package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/spyboss/invoiceapp/internal/buildinfo"
	"github.com/spyboss/invoiceapp/internal/httpx"
)

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
}

// healthz answers the platform healthcheck. It fails when the database is
// unreachable, because a server that cannot read is not healthy.
func (s *Server) healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		resp := healthResponse{
			Status:   "ok",
			Database: "ok",
			Version:  buildinfo.Version,
			Commit:   buildinfo.Commit(),
		}
		status := http.StatusOK
		if err := s.ping(ctx); err != nil {
			resp.Status = "degraded"
			resp.Database = "unreachable"
			status = http.StatusServiceUnavailable
			s.log.LogAttrs(ctx, slog.LevelWarn, "healthcheck database ping failed",
				slog.Any("error", err),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
		}
		writeJSON(w, status, resp)
	}
}

// writeJSON is the only json writer in the app. Errors from encoding a struct
// of strings cannot be fixed after the headers are sent, so they are logged by
// the caller's request line and nothing more.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
