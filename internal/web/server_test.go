package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
)

var testTenant = store.Tenant{
	ID:     uuid.MustParse("0199a000-0000-7000-8000-000000000001"),
	Slug:   "test-motors",
	Name:   "Test Motors",
	Domain: "test.local",
}

func testHandler(ping func(context.Context) error) http.Handler {
	return testHandlerResolving(ping, func(context.Context, string) (store.Tenant, error) {
		return testTenant, nil
	})
}

func testHandlerResolving(ping func(context.Context) error, resolve func(context.Context, string) (store.Tenant, error)) http.Handler {
	return New(Deps{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Ping:          ping,
		ResolveTenant: resolve,
		Now:           func() time.Time { return time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC) },
	})
}

func TestHealthzOK(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type = %q", ct)
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" || body.Database != "ok" {
		t.Errorf("body = %+v", body)
	}
	if body.Version == "" || body.Commit == "" {
		t.Errorf("version/commit empty: %+v", body)
	}
}

func TestHealthzDatabaseDown(t *testing.T) {
	h := testHandler(func(context.Context) error { return errors.New("connection refused") })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "degraded" || body.Database != "unreachable" {
		t.Errorf("body = %+v", body)
	}
}

func TestHealthzNoStore(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache control = %q, want no-store", got)
	}
}

func TestNotFoundIsHTML(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Page not found") {
		t.Errorf("body = %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request id") {
		t.Errorf("body lacks request id: %q", rec.Body.String())
	}
}

func TestUnknownHostSaysSo(t *testing.T) {
	h := testHandlerResolving(func(context.Context) error { return nil },
		func(context.Context, string) (store.Tenant, error) {
			return store.Tenant{}, tenant.ErrUnknownHost
		})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No site at this address") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestStaticIsServed(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "--pico-primary") {
		t.Error("app.css body does not look like our stylesheet")
	}
}

func TestPostWithoutCSRFIsRejected(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("email=a@b.c")))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Form expired") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestAnonymousHomeRedirectsToLogin(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Errorf("location = %q, want /login", got)
	}
}

func TestHealthzRejectsPost(t *testing.T) {
	h := testHandler(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code == http.StatusOK {
		t.Fatal("POST /healthz returned 200")
	}
}
