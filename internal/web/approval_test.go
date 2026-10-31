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

// mustUUID parses an id that came from a redirect location, failing the test
// if it is not a real uuid.
func mustUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("id %q is not a uuid: %v", raw, err)
	}
	return id
}

// createUserWithRole adds a second person to a tenant with a real password, so
// a test can sign them in or use them as a manager pin.
func createUserWithRole(t *testing.T, tn store.Tenant, email, name, role, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	err = webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		_, err := q.CreateUser(context.Background(), store.CreateUserParams{
			TenantID:     tn.ID,
			Email:        email,
			Name:         name,
			PasswordHash: hash,
			Role:         role,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create %s: %v", role, err)
	}
}

// discountedSale builds the form for a cash sale priced 20%% below the listed
// 1,250,000 unit, which is past a 15%% discount limit.
func discountedSale(c *testClient, customerID, vehicleID string) url.Values {
	form := documentForm(c, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,000,000",
		"down":        "1",
	})
	return form
}

// TestDiscountPastLimitNeedsAManagerPin proves C8: a price below the tenant's
// discount limit cannot be issued by a clerk alone. The form comes back with
// the rule that tripped and the pin block open; a wrong pin or a clerk
// without document.approve is refused; a manager's own password signs it off
// and both people end up on the document.
func TestDiscountPastLimitNeedsAManagerPin(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	createUserWithRole(t, tn, "manager@alpha.local", "Sales Manager", "MANAGER", "manager pin one")
	createUserWithRole(t, tn, "sales@alpha.local", "Counter Clerk", "SALES", "clerk pin one")

	// The owner sets the rules: a discount past 15%% needs a pin, and the
	// backdate limit is generous so only the discount rule trips here.
	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	{
		token := owner.csrfFrom(owner.get("/settings"))
		rec := owner.post("/settings", url.Values{
			"csrf_token":             {token},
			"company_name":           {"Alpha Motors"},
			"discount_limit_percent": {"15"},
			"variance_alert_cents":   {"5000"},
			"backdate_days":          {"30"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("save settings = %d:\n%s", rec.Code, rec.Body.String())
		}
	}

	// The clerk issues a discounted sale. 20%% off a 1,250,000 unit is past
	// the 15%% limit, so the form comes back with the pin open.
	customerID := createCustomerHTTP(t, owner, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, owner, "ZAPM0000007001", "1,250,000")
	clerk := signIn(t, h, "alpha.local", "sales@alpha.local", "clerk pin one")

	form := discountedSale(clerk, customerID, vehicleID)
	rec := clerk.post("/documents", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clerk posts a discount past the limit = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"This price is 20% below the listed price",
		"limit without approval is 15%",
		"Manager approval",
		`name="approver_email"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pin form missing %q:\n%s", want, body)
		}
	}

	// A wrong manager password is refused, and the email is kept on the form.
	form.Set("approver_email", "manager@alpha.local")
	form.Set("approver_password", "not the pin")
	rec = clerk.post("/documents", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong pin = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "password did not match") {
		t.Errorf("wrong pin body missing the refusal:\n%s", rec.Body.String())
	}

	// The clerk cannot sign off with their own credentials: SALES has no
	// document.approve, so the pin is refused even though the password is
	// the clerk's own and correct.
	form.Set("approver_email", "sales@alpha.local")
	form.Set("approver_password", "clerk pin one")
	rec = clerk.post("/documents", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clerk pins themself = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not allowed to approve documents") {
		t.Errorf("clerk self-pin body missing the refusal:\n%s", rec.Body.String())
	}

	// A manager's own password signs it off.
	form.Set("approver_email", "manager@alpha.local")
	form.Set("approver_password", "manager pin one")
	form.Set("approval_note", "Agreed with the owner on the phone")
	rec = clerk.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("manager pin = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	docID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")

	// The document records the manager as approver, so the clerk and the
	// manager both end up on the paper.
	var doc store.Document
	if err := webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		var err error
		doc, err = q.DocumentByID(context.Background(), tn.ID, mustUUID(t, docID))
		return err
	}); err != nil {
		t.Fatalf("read document: %v", err)
	}
	if doc.ApprovedBy == nil {
		t.Fatal("document has no approver recorded")
	}
	var approver store.User
	if err := webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		var err error
		approver, err = q.UserByID(context.Background(), tn.ID, *doc.ApprovedBy)
		return err
	}); err != nil {
		t.Fatalf("read approver: %v", err)
	}
	if approver.Email != "manager@alpha.local" {
		t.Errorf("approver = %q, want manager@alpha.local", approver.Email)
	}
	if doc.ApprovalNote != "Agreed with the owner on the phone" {
		t.Errorf("approval note = %q, want the reason the manager gave", doc.ApprovalNote)
	}

	// The detail page shows who approved it.
	page := clerk.get("/documents/" + docID)
	if page.Code != http.StatusOK {
		t.Fatalf("GET document = %d:\n%s", page.Code, page.Body.String())
	}
	for _, want := range []string{"Approved by", "Sales Manager", "Agreed with the owner on the phone"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("document page missing %q:\n%s", want, page.Body.String())
		}
	}
}

// TestBackdatePastLimitNeedsAPin proves C9: a document dated beyond the
// tenant's backdate window needs a pin even when the price is full.
func TestBackdatePastLimitNeedsAPin(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	createUserWithRole(t, tn, "manager@alpha.local", "Sales Manager", "MANAGER", "manager pin one")
	createUserWithRole(t, tn, "sales@alpha.local", "Counter Clerk", "SALES", "clerk pin one")

	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	{
		token := owner.csrfFrom(owner.get("/settings"))
		rec := owner.post("/settings", url.Values{
			"csrf_token":             {token},
			"company_name":           {"Alpha Motors"},
			"discount_limit_percent": {"15"},
			"variance_alert_cents":   {"5000"},
			"backdate_days":          {"3"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("save settings = %d:\n%s", rec.Code, rec.Body.String())
		}
	}

	customerID := createCustomerHTTP(t, owner, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, owner, "ZAPM0000008001", "1,250,000")
	clerk := signIn(t, h, "alpha.local", "sales@alpha.local", "clerk pin one")

	// Full price, but dated a week ago: past the 3 day limit.
	form := documentForm(clerk, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
		"down":        "1",
	})
	form.Set("doc_date", "2000-01-01")
	rec := clerk.post("/documents", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("backdate past the limit = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dated") || !strings.Contains(rec.Body.String(), "days back") {
		t.Errorf("backdate pin form missing the reason:\n%s", rec.Body.String())
	}
}

// TestVoidKeepsPaperReturnsUnitAndAlerts proves C4 and C12: a void is not a
// delete. The paper and its number stay on the record with the reason and who
// gave it, the unit goes back to the yard, and a VOID alert is written in the
// same transaction.
func TestVoidKeepsPaperReturnsUnitAndAlerts(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	customerID := createCustomerHTTP(t, owner, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, owner, "ZAPM0000009001", "1,250,000")

	// A full-price cash sale: no pin needed.
	form := documentForm(owner, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
		"down":        "1",
	})
	rec := owner.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("issue sale = %d:\n%s", rec.Code, rec.Body.String())
	}
	docID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")

	// The sale took the unit off the yard.
	assertVehicleStatus(t, tn.ID, vehicleID, store.StatusSold)

	// The void form needs a reason. A refusal comes back as the document page
	// with the reason on it, so the person sees why and the box stays put.
	token := owner.csrfFrom(owner.get("/documents/" + docID))
	rec = owner.post("/documents/"+docID+"/void", url.Values{"csrf_token": {token}, "reason": {""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("void without a reason = %d, want the page back:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Give a reason before voiding") {
		t.Errorf("void-without-reason page missing the refusal:\n%s", rec.Body.String())
	}

	// With a reason the void lands: the paper is kept, the unit returns.
	rec = owner.post("/documents/"+docID+"/void", url.Values{
		"csrf_token": {token},
		"reason":     {"Customer changed their mind before delivery"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("void = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}

	// The paper still exists, marked void, with the reason and who gave it.
	page := owner.get("/documents/" + docID)
	if page.Code != http.StatusOK {
		t.Fatalf("GET voided document = %d:\n%s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, want := range []string{"VOID", "Voided by", "Customer changed their mind before delivery"} {
		if !strings.Contains(body, want) {
			t.Errorf("voided document page missing %q:\n%s", want, body)
		}
	}

	// The unit is back in the yard.
	assertVehicleStatus(t, tn.ID, vehicleID, store.StatusAvailable)

	// A VOID alert was recorded in the same transaction.
	var alerts []store.Alert
	if err := webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		var err error
		alerts, err = q.ListAlerts(context.Background(), tn.ID, 50)
		return err
	}); err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	found := false
	for _, a := range alerts {
		if a.Kind == store.AlertVoid && strings.Contains(a.Message, "changed their mind") {
			found = true
		}
	}
	if !found {
		t.Errorf("no VOID alert recorded, alerts = %+v", alerts)
	}
}

// TestVoidOnClosedDayIsRefused proves a void is dated the day it happens and
// cannot land on a day whose z is already closed.
func TestVoidOnClosedDayIsRefused(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	customerID := createCustomerHTTP(t, owner, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, owner, "ZAPM0000009101", "1,250,000")
	form := documentForm(owner, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
		"down":        "1",
	})
	rec := owner.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("issue sale = %d:\n%s", rec.Code, rec.Body.String())
	}
	docID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")

	// Close the day.
	token := owner.csrfFrom(owner.get("/day-end"))
	rec = owner.post("/day-end", url.Values{
		"csrf_token":    {token},
		"opening_float": {"0"},
		"counted":       {"0"},
		"notes":         {""},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("close day = %d:\n%s", rec.Code, rec.Body.String())
	}

	// A void is dated today, which is now closed, so it is refused with the
	// reason on the page.
	token = owner.csrfFrom(owner.get("/documents/" + docID))
	rec = owner.post("/documents/"+docID+"/void", url.Values{
		"csrf_token": {token},
		"reason":     {"Too late, the day is shut"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("void on a closed day = %d, want the page back:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already closed") {
		t.Errorf("closed-day void body missing the reason:\n%s", rec.Body.String())
	}

	// The unit is still sold, because the void did not land.
	assertVehicleStatus(t, tn.ID, vehicleID, store.StatusSold)
}

// TestAlertsPageListsAndIsGated proves the alerts screen shows what has fired
// and is only reachable by a role that holds alert.view.
func TestAlertsPageListsAndIsGated(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	createUserWithRole(t, tn, "sales@alpha.local", "Counter Clerk", "SALES", "clerk pin one")
	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// Nothing has fired yet.
	rec := owner.get("/alerts")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /alerts = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Nothing has fired") {
		t.Fatalf("empty alerts page unexpected:\n%s", rec.Body.String())
	}

	// A void records an alert, which the page then lists.
	customerID := createCustomerHTTP(t, owner, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, owner, "ZAPM0000009201", "1,250,000")
	form := documentForm(owner, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
		"down":        "1",
	})
	rec = owner.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("issue sale = %d:\n%s", rec.Code, rec.Body.String())
	}
	docID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")
	token := owner.csrfFrom(owner.get("/documents/" + docID))
	rec = owner.post("/documents/"+docID+"/void", url.Values{
		"csrf_token": {token},
		"reason":     {"Wrong unit handed over"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("void = %d:\n%s", rec.Code, rec.Body.String())
	}

	rec = owner.get("/alerts")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /alerts after a void = %d:\n%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"Void", "Wrong unit handed over"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("alerts page missing %q:\n%s", want, rec.Body.String())
		}
	}

	// The nav shows Alerts to the owner, who holds alert.view.
	if !strings.Contains(rec.Body.String(), `href="/alerts"`) {
		t.Errorf("owner nav missing the Alerts entry:\n%s", rec.Body.String())
	}

	// A clerk has no alert.view, so /alerts is refused and the nav hides it.
	clerk := signIn(t, h, "alpha.local", "sales@alpha.local", "clerk pin one")
	rec = clerk.get("/alerts")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("clerk GET /alerts = %d, want 403:\n%s", rec.Code, rec.Body.String())
	}
	home := clerk.get("/")
	if strings.Contains(home.Body.String(), `href="/alerts"`) {
		t.Errorf("clerk nav shows Alerts without alert.view:\n%s", home.Body.String())
	}
}

// assertVehicleStatus reads a vehicle's status straight from the store, so a
// test can prove a sale moved stock and a void moved it back.
func assertVehicleStatus(t *testing.T, tnID uuid.UUID, vehicleID, want string) {
	t.Helper()
	var v store.Vehicle
	if err := webStore.InTenant(context.Background(), tnID, func(q *store.Store) error {
		var err error
		v, err = q.VehicleByID(context.Background(), tnID, mustUUID(t, vehicleID))
		return err
	}); err != nil {
		t.Fatalf("read vehicle: %v", err)
	}
	if v.Status != want {
		t.Errorf("vehicle status = %q, want %q", v.Status, want)
	}
}
