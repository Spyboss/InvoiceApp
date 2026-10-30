package web

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// render writes an html response. A template error after the headers are sent
// cannot become an error page, so it is logged with the request id instead.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		s.log.LogAttrs(r.Context(), slog.LevelError, "render failed",
			slog.Any("error", err),
			slog.String("request_id", httpx.RequestID(r.Context())),
			slog.String("path", r.URL.Path),
		)
	}
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	s.render(w, r, status, views.ErrorPage(title, detail, httpx.RequestID(r.Context())))
}
