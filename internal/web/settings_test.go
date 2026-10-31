package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSettings(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// Owner can open the settings screen.
	rec := c.get("/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Settings") {
		t.Fatalf("settings page missing title:\n%s", rec.Body.String())
	}

	// Owner saves settings. CSRF token comes from the page itself.
	token := c.csrfFrom(rec)
	form := url.Values{
		"csrf_token":             {token},
		"company_name":           {"Alpha Motors Lanka"},
		"address":                {"Kandy Road, Colombo"},
		"phone":                  {"0112345678"},
		"email":                  {"info@alpha.local"},
		"vat_number":             {"123456789"},
		"discount_limit_percent": {"15"},
		"variance_alert_cents":   {"5000.50"},
		"backdate_days":          {"3"},
		"alert_email":            {"alerts@alpha.local"},
		"alert_telegram_chat":    {"@alphamotors"},
	}
	rec = c.post("/settings", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /settings = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/settings?saved=1" {
		t.Errorf("location = %q, want /settings?saved=1", loc)
	}

	// The settings are now on the page.
	rec = c.get("/settings")
	for _, want := range []string{"Alpha Motors Lanka", "15", "5000.50", "3", "alerts@alpha.local", "@alphamotors"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("settings page missing %q:\n%s", want, rec.Body.String())
		}
	}

	// A discount percent over 100 is refused.
	form["discount_limit_percent"] = []string{"101"}
	form["csrf_token"] = []string{c.csrfFrom(rec)}
	rec = c.post("/settings", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("discount over 100 = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "0-100") {
		t.Errorf("discount error body missing explanation:\n%s", rec.Body.String())
	}
}
