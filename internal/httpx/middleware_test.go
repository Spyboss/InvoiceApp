package httpx

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequestIDGeneratesAndPropagates(t *testing.T) {
	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
	}), WithRequestID())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("request id missing from context")
	}
	if got := rec.Header().Get("X-Request-Id"); got != seen {
		t.Errorf("response header %q, context %q", got, seen)
	}
}

func TestRequestIDHonoursValidInboundID(t *testing.T) {
	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
	}), WithRequestID())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "abc12345")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "abc12345" {
		t.Errorf("request id = %q, want abc12345", seen)
	}
}

func TestRequestIDRejectsGarbage(t *testing.T) {
	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
	}), WithRequestID())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "bad id with spaces")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen == "bad id with spaces" {
		t.Fatal("invalid inbound id was trusted")
	}
	if seen == "" {
		t.Fatal("no replacement id generated")
	}
}

func TestRealIP(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{"cloudflare header wins", map[string]string{"CF-Connecting-IP": "203.0.113.9", "X-Forwarded-For": "10.0.0.1"}, "10.0.0.2:5555", "203.0.113.9"},
		{"first forwarded entry", map[string]string{"X-Forwarded-For": "198.51.100.7, 10.0.0.1"}, "10.0.0.2:5555", "198.51.100.7"},
		{"remote addr fallback", nil, "192.0.2.4:5555", "192.0.2.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = ClientIP(r.Context())
			}), RealIP())

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.want {
				t.Errorf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRecovererReturns500(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}), WithRequestID(), Recoverer(discardLogger()))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "request id") {
		t.Errorf("body %q lacks request id", rec.Body.String())
	}
}

func TestRecovererLetsErrAbortHandlerThrough(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}), Recoverer(discardLogger()))

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestSecureHeaders(t *testing.T) {
	h := Chain(okHandler(), SecureHeaders())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for key, value := range want {
		if got := rec.Header().Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("csp = %q", csp)
	}
	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("hsts on plain http = %q, want empty", hsts)
	}
}

func TestSecureHeadersHSTSBehindProxy(t *testing.T) {
	h := Chain(okHandler(), SecureHeaders())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts == "" {
		t.Error("hsts missing on https request")
	}
}

func TestLoggerRecordsStatus(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	}), WithRequestID(), Logger(log))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/kettle", nil))

	out := buf.String()
	for _, want := range []string{`"status":418`, `"path":"/kettle"`, `"request_id"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q missing %s", out, want)
		}
	}
}
