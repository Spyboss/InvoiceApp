package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/db"
	"github.com/spyboss/invoiceapp/internal/db/dbtest"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/tenant"
)

var (
	adminPool *pgxpool.Pool
	webStore  *store.Store
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	var release func()
	if dsn != "" {
		ctx := context.Background()

		var err error
		release, err = dbtest.Lock(ctx, dsn)
		if err != nil {
			panic(err)
		}

		adminPool, err = db.OpenAdmin(ctx, dsn)
		if err != nil {
			panic(err)
		}
		if _, err := db.Migrate(ctx, adminPool); err != nil {
			panic(err)
		}

		pool, err := db.Open(ctx, dsn)
		if err != nil {
			panic(err)
		}
		webStore = store.New(pool)
	}

	code := m.Run()

	if adminPool != nil {
		adminPool.Close()
	}
	if release != nil {
		release()
	}
	os.Exit(code)
}

// testClock lets a test move time forward without sleeping.
type testClock struct {
	at time.Time
}

func (c *testClock) Now() time.Time { return c.at }

func (c *testClock) Advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestServer(t *testing.T) (http.Handler, *testClock) {
	t.Helper()
	if webStore == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if _, err := adminPool.Exec(context.Background(),
		`truncate table audit_log, sessions, user_permissions, users, tenants cascade`); err != nil {
		t.Fatalf("reset: %v", err)
	}

	clock := &testClock{at: time.Now().UTC()}
	h := New(Deps{
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store: webStore,
		Ping:  func(context.Context) error { return nil },
		ResolveTenant: func(ctx context.Context, host string) (store.Tenant, error) {
			tn, err := webStore.TenantByDomain(ctx, tenant.NormaliseHost(host))
			if errors.Is(err, store.ErrNotFound) {
				return store.Tenant{}, tenant.ErrUnknownHost
			}
			return tn, err
		},
		Now: clock.Now,
	})
	return h, clock
}

type testClient struct {
	t       *testing.T
	h       http.Handler
	host    string
	cookies map[string]string
}

func newTestClient(t *testing.T, h http.Handler, host string) *testClient {
	return &testClient{t: t, h: h, host: host, cookies: make(map[string]string)}
}

func (c *testClient) get(path string) *httptest.ResponseRecorder {
	return c.do(http.MethodGet, path, nil)
}

func (c *testClient) post(path string, form url.Values) *httptest.ResponseRecorder {
	return c.do(http.MethodPost, path, form)
}

func (c *testClient) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Host = c.host
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for name, value := range c.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)

	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 || cookie.Value == "" {
			delete(c.cookies, cookie.Name)
			continue
		}
		c.cookies[cookie.Name] = cookie.Value
	}
	return rec
}

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (c *testClient) csrfFrom(rec *httptest.ResponseRecorder) string {
	c.t.Helper()
	m := csrfPattern.FindStringSubmatch(rec.Body.String())
	if m == nil {
		c.t.Fatalf("no csrf token in response body:\n%s", rec.Body.String())
	}
	return m[1]
}

func createTenantAndOwner(t *testing.T, name, domain, email, password string) store.Tenant {
	t.Helper()
	ctx := context.Background()

	tn, err := webStore.CreateTenant(ctx, strings.ToLower(strings.ReplaceAll(name, " ", "-")), name, domain)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	err = webStore.InTenant(ctx, tn.ID, func(q *store.Store) error {
		_, err := q.CreateUser(ctx, store.CreateUserParams{
			TenantID:     tn.ID,
			Email:        email,
			Name:         "Test Owner",
			PasswordHash: hash,
			Role:         "OWNER",
		})
		return err
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return tn
}

func TestSetupCreatesOwnerAndSignsIn(t *testing.T) {
	h, _ := newTestServer(t)
	c := newTestClient(t, h, "new.local")

	rec := c.get("/setup")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Set up this dealership") {
		t.Fatalf("unexpected setup page:\n%s", rec.Body.String())
	}
	token := c.csrfFrom(rec)

	short := url.Values{
		"csrf_token": {token},
		"dealership": {"Tangalle Motors"},
		"name":       {"Uminda"},
		"email":      {"owner@example.com"},
		"password":   {"short"},
		"confirm":    {"short"},
	}
	rec = c.post("/setup", short)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short password = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "at least 12 characters") {
		t.Errorf("body does not explain the policy:\n%s", rec.Body.String())
	}

	const password = "correct horse battery staple"
	form := url.Values{
		"csrf_token": {token},
		"dealership": {"Tangalle Motors"},
		"name":       {"Uminda"},
		"email":      {"Owner@Example.com"},
		"password":   {password},
		"confirm":    {password},
	}
	rec = c.post("/setup", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /setup = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("location = %q, want /", loc)
	}
	if c.cookies["session"] == "" {
		t.Fatal("no session cookie after setup")
	}

	rec = c.get("/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	for _, want := range []string{"Uminda", "owner@example.com", "OWNER", "audit.view"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("home page missing %q:\n%s", want, rec.Body.String())
		}
	}

	rec = c.get("/audit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audit = %d, want 200", rec.Code)
	}
	for _, want := range []string{"tenant.create", "user.create", "auth.login"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("audit page missing %q:\n%s", want, rec.Body.String())
		}
	}

	token = c.csrfFrom(c.get("/"))
	rec = c.post("/logout", url.Values{"csrf_token": {token}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout = %d, want 303", rec.Code)
	}

	rec = c.get("/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / after logout = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("location = %q, want /login", loc)
	}

	// A second host cannot bootstrap another dealership through the public
	// setup screen.
	other := newTestClient(t, h, "other.local")
	rec = other.get("/setup")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second bootstrap = %d, want 404", rec.Code)
	}
}

func TestLockoutAfterFiveFailures(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)

	c := newTestClient(t, h, "alpha.local")
	token := c.csrfFrom(c.get("/login"))

	wrong := url.Values{
		"csrf_token": {token},
		"email":      {"owner@alpha.local"},
		"password":   {"not the password"},
	}
	for i := 0; i < 5; i++ {
		rec := c.post("/login", wrong)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d = %d, want 200", i+1, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Email or password is incorrect") {
			t.Fatalf("attempt %d body:\n%s", i+1, rec.Body.String())
		}
	}

	// Right password, still locked.
	good := url.Values{
		"csrf_token": {token},
		"email":      {"owner@alpha.local"},
		"password":   {password},
	}
	rec := c.post("/login", good)
	if !strings.Contains(rec.Body.String(), "locked for a few minutes") {
		t.Fatalf("locked login body:\n%s", rec.Body.String())
	}
	if c.cookies["session"] != "" {
		t.Fatal("locked account got a session")
	}

	clock.Advance(16 * time.Minute)

	rec = c.post("/login", good)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login after lock = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if c.cookies["session"] == "" {
		t.Fatal("no session cookie after successful login")
	}

	rec = c.get("/audit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audit = %d, want 200", rec.Code)
	}
	if got := strings.Count(rec.Body.String(), "auth.login_failed"); got < 5 {
		t.Errorf("audit has %d failed logins, want at least 5", got)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)

	c := newTestClient(t, h, "alpha.local")
	token := c.csrfFrom(c.get("/login"))

	rec := c.post("/login", url.Values{
		"csrf_token": {token},
		"email":      {"nobody@alpha.local"},
		"password":   {password},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown email = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Email or password is incorrect") {
		t.Fatalf("unknown email body:\n%s", rec.Body.String())
	}
	if c.cookies["session"] != "" {
		t.Fatal("unknown email got a session")
	}
}

func TestSessionCookieIsRequiredForHome(t *testing.T) {
	h, _ := newTestServer(t)
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", "correct horse battery staple")

	c := newTestClient(t, h, "alpha.local")
	c.cookies["session"] = "forged-token-value"
	rec := c.get("/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("forged cookie = %d, want 303", rec.Code)
	}
}
