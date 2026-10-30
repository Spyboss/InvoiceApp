package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/store"
)

func signIn(t *testing.T, h http.Handler, host, email, password string) *testClient {
	t.Helper()
	c := newTestClient(t, h, host)
	token := c.csrfFrom(c.get("/login"))
	rec := c.post("/login", url.Values{
		"csrf_token": {token},
		"email":      {email},
		"password":   {password},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign in as %s = %d, want 303:\n%s", email, rec.Code, rec.Body.String())
	}
	return c
}

func idFromLocation(t *testing.T, location, prefix string) string {
	t.Helper()
	rest := strings.TrimPrefix(location, prefix)
	id := strings.SplitN(rest, "?", 2)[0]
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("location %q does not carry an id: %v", location, err)
	}
	return id
}

func TestCustomerAndVehicleFlow(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// Customer list starts empty.
	rec := c.get("/customers")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /customers = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No customers match") {
		t.Fatalf("empty list body:\n%s", rec.Body.String())
	}

	token := c.csrfFrom(c.get("/customers/new"))
	rec = c.post("/customers", url.Values{
		"csrf_token": {token},
		"name":       {"Saman Perera"},
		"nic":        {"907654321v"},
		"phone":      {"0771234567"},
		"address":    {"Beliatta Road, Tangalle"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /customers = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	customerID := idFromLocation(t, rec.Header().Get("Location"), "/customers/")

	// The NIC is normalised to upper case, so the index sees one person.
	rec = c.get("/customers/" + customerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET customer = %d, want 200", rec.Code)
	}
	for _, want := range []string{"Saman Perera", "907654321V", "0771234567"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("customer page missing %q", want)
		}
	}

	// A second customer with the same NIC is refused, whatever the case.
	token = c.csrfFrom(c.get("/customers/new"))
	rec = c.post("/customers", url.Values{
		"csrf_token": {token},
		"name":       {"Saman P."},
		"nic":        {"907654321V"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate nic = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Fatalf("duplicate nic body:\n%s", rec.Body.String())
	}

	// Edit keeps the person and changes the name.
	token = c.csrfFrom(c.get("/customers/" + customerID + "/edit"))
	rec = c.post("/customers/"+customerID, url.Values{
		"csrf_token": {token},
		"name":       {"Saman Perera Silva"},
		"nic":        {"907654321V"},
		"phone":      {"0771234567"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST customer update = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(c.get("/customers/"+customerID).Body.String(), "Saman Perera Silva") {
		t.Error("customer update was not saved")
	}

	// Intake one unit.
	token = c.csrfFrom(c.get("/vehicles/new"))
	rec = c.post("/vehicles", url.Values{
		"csrf_token":  {token},
		"make":        {"Piaggio"},
		"model":       {"APE City"},
		"chassis_no":  {"zapm1234567890"},
		"engine_no":   {"ENG-778"},
		"colour":      {"White"},
		"yom":         {"2026"},
		"price":       {"1,250,000.00"},
		"cost":        {"1,100,000"},
		"received_at": {"2026-10-30"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /vehicles = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	vehicleID := idFromLocation(t, rec.Header().Get("Location"), "/vehicles/")

	rec = c.get("/vehicles/" + vehicleID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET vehicle = %d, want 200", rec.Code)
	}
	for _, want := range []string{"APE City", "ZAPM1234567890", "1,250,000.00", "AVAILABLE"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("vehicle page missing %q", want)
		}
	}

	// The same chassis cannot enter stock twice, even lower case.
	token = c.csrfFrom(c.get("/vehicles/new"))
	rec = c.post("/vehicles", url.Values{
		"csrf_token":  {token},
		"make":        {"Piaggio"},
		"model":       {"APE City"},
		"chassis_no":  {"ZAPM1234567890"},
		"received_at": {"2026-10-30"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate chassis = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already in stock") {
		t.Fatalf("duplicate chassis body:\n%s", rec.Body.String())
	}

	// Reserve it. The ledger does not move, because the unit is still here.
	token = c.csrfFrom(c.get("/vehicles/" + vehicleID))
	rec = c.post("/vehicles/"+vehicleID+"/status", url.Values{
		"csrf_token": {token},
		"status":     {"RESERVED"},
		"note":       {"advance paid"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reserve = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(c.get("/vehicles/"+vehicleID).Body.String(), "RESERVED") {
		t.Error("vehicle is not reserved")
	}

	// The stock screen agrees with itself.
	rec = c.get("/stock")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stock = %d, want 200", rec.Code)
	}
	for _, want := range []string{"On hand", "1 units received, minus sold", "intake", "APE City"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("stock page missing %q:\n%s", want, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "disagree") {
		t.Errorf("stock page reports a ledger mismatch:\n%s", rec.Body.String())
	}

	// Every write is in the trail.
	rec = c.get("/audit")
	for _, want := range []string{"customer.create", "customer.update", "vehicle.create", "stock.reserve"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("audit missing %q", want)
		}
	}
}

func TestSalesCanReserveButCannotSetMoney(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	err = webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		_, err := q.CreateUser(context.Background(), store.CreateUserParams{
			TenantID:     tn.ID,
			Email:        "sales@alpha.local",
			Name:         "Sales Person",
			PasswordHash: hash,
			Role:         "SALES",
		})
		return err
	})
	if err != nil {
		t.Fatalf("create sales user: %v", err)
	}

	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	token := owner.csrfFrom(owner.get("/vehicles/new"))
	rec := owner.post("/vehicles", url.Values{
		"csrf_token":  {token},
		"make":        {"Piaggio"},
		"model":       {"APE City"},
		"chassis_no":  {"ZAPM9999999999"},
		"cost":        {"1,100,000"},
		"price":       {"1,250,000"},
		"received_at": {"2026-10-30"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("owner intake = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	vehicleID := idFromLocation(t, rec.Header().Get("Location"), "/vehicles/")

	sales := signIn(t, h, "alpha.local", "sales@alpha.local", password)

	// Sales can see the yard and add customers.
	for _, path := range []string{"/vehicles", "/vehicles/" + vehicleID, "/customers", "/customers/new", "/stock"} {
		if rec := sales.get(path); rec.Code != http.StatusOK {
			t.Errorf("sales GET %s = %d, want 200", path, rec.Code)
		}
	}

	// Sales cannot open intake or read the audit trail.
	for _, path := range []string{"/vehicles/new", "/vehicles/" + vehicleID + "/edit", "/audit"} {
		if rec := sales.get(path); rec.Code != http.StatusForbidden {
			t.Errorf("sales GET %s = %d, want 403", path, rec.Code)
		}
	}

	// The cost price is not on the page sales can open.
	if body := sales.get("/vehicles/" + vehicleID).Body.String(); strings.Contains(body, "Cost") {
		t.Error("sales can see the cost price")
	}

	// Sales can reserve the unit.
	token = sales.csrfFrom(sales.get("/vehicles/" + vehicleID))
	rec = sales.post("/vehicles/"+vehicleID+"/status", url.Values{
		"csrf_token": {token},
		"status":     {"RESERVED"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sales reserve = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}

	if body := sales.get("/audit"); body.Code == http.StatusOK {
		t.Fatal("sales reached the audit page")
	}
}
