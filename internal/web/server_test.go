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
)

func testServer(ping func(context.Context) error) http.Handler {
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), ping)
}

func TestHealthzOK(t *testing.T) {
	h := testServer(func(context.Context) error { return nil })

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
	h := testServer(func(context.Context) error { return errors.New("connection refused") })

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
	h := testServer(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache control = %q, want no-store", got)
	}
}

func TestNotFoundIsHTML(t *testing.T) {
	h := testServer(func(context.Context) error { return nil })

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

func TestStaticIsServed(t *testing.T) {
	h := testServer(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "--pico-primary") {
		t.Error("app.css body does not look like our stylesheet")
	}
}

func TestHealthzRejectsPost(t *testing.T) {
	h := testServer(func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code == http.StatusOK {
		t.Fatal("POST /healthz returned 200")
	}
}
