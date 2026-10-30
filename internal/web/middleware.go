package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/tenant"
)

// tenantHandler resolves the dealership for the request host. Setup is
// allowed through without a tenant so a brand new host can be provisioned
// once.
func (s *Server) tenantHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenantFreePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		t, err := s.resolver(r.Context(), r.Host)
		if errors.Is(err, tenant.ErrUnknownHost) {
			if r.URL.Path == "/setup" {
				next.ServeHTTP(w, r)
				return
			}
			s.renderError(w, r, http.StatusNotFound, "No site at this address",
				"Nothing is configured for this hostname yet.")
			return
		}
		if err != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "tenant resolution failed",
				slog.Any("error", err),
				slog.String("host", r.Host),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
			s.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
				"The site could not be loaded. The error has been logged.")
			return
		}

		next.ServeHTTP(w, r.WithContext(tenant.WithTenant(r.Context(), t)))
	})
}

func tenantFreePath(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/static/")
}

// sessionHandler turns a session cookie into a signed in user. A missing or
// expired session is anonymous, not an error.
func (s *Server) sessionHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, ok := tenant.FromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		token := s.readCookie(r, s.sessionCookieName())
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		signedIn, err := s.auth.Authenticate(r.Context(), t.ID, token)
		if errors.Is(err, auth.ErrNoSession) {
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			s.log.LogAttrs(r.Context(), slog.LevelError, "session load failed",
				slog.Any("error", err),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), signedIn)))
	})
}

type csrfKey struct{}

// csrfHandler implements the double submit cookie: a random httpOnly cookie
// plus the same value in the form. Unsafe methods must match.
func (s *Server) csrfHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenantFreePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token := s.readCookie(r, s.csrfCookieName())
		if len(token) < 32 {
			fresh, err := auth.NewCSRFToken()
			if err != nil {
				s.renderError(w, r, http.StatusInternalServerError, "Something went wrong", "Could not start a secure session.")
				return
			}
			token = fresh
			s.setCSRFCookie(w, token)
		}

		if !safeMethod(r.Method) {
			if err := r.ParseForm(); err != nil {
				s.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
				return
			}
			if !auth.CSRFValid(token, r.PostFormValue("csrf_token")) {
				s.renderError(w, r, http.StatusForbidden, "Form expired",
					"Reload the page and try again.")
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, token)))
	})
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func csrfToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfKey{}).(string)
	return token
}

// requireUser sends anonymous visitors to the login page.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.User(r.Context()); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requirePermission refuses a signed in user who lacks the permission.
func (s *Server) requirePermission(permission string, next http.Handler) http.Handler {
	return s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.Can(r.Context(), permission) {
			s.renderError(w, r, http.StatusForbidden, "Not allowed",
				"Your account does not have access to this page.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
