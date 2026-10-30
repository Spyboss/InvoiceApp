package web

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/store"
)

// fakePDF stands in for Gotenberg. The bytes are not a real pdf, but they
// satisfy the archive's size and hash checks.
type fakePDF struct {
	mu    sync.Mutex
	calls int
	bytes []byte
}

func newFakePDF() *fakePDF {
	return &fakePDF{bytes: append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 1200)...)}
}

func (f *fakePDF) Render(ctx context.Context, tenantName string, doc store.Document) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.bytes, nil
}

func (f *fakePDF) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func createCustomerHTTP(t *testing.T, c *testClient, name, nic string) string {
	t.Helper()
	token := c.csrfFrom(c.get("/customers/new"))
	rec := c.post("/customers", url.Values{
		"csrf_token": {token},
		"name":       {name},
		"nic":        {nic},
		"phone":      {"0771234567"},
		"address":    {"Beliatta Road, Tangalle"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create customer = %d:\n%s", rec.Code, rec.Body.String())
	}
	return idFromLocation(t, rec.Header().Get("Location"), "/customers/")
}

func createVehicleHTTP(t *testing.T, c *testClient, chassis, price string) string {
	t.Helper()
	token := c.csrfFrom(c.get("/vehicles/new"))
	rec := c.post("/vehicles", url.Values{
		"csrf_token":   {token},
		"make":         {"Piaggio"},
		"model":        {"APE City"},
		"vehicle_type": {"Petrol"},
		"chassis_no":   {chassis},
		"engine_no":    {"ENG-1234"},
		"colour":       {"White"},
		"yom":          {"2026"},
		"price":        {price},
		"cost":         {"1000000"},
		"received_at":  {"2026-10-30"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create vehicle = %d:\n%s", rec.Code, rec.Body.String())
	}
	return idFromLocation(t, rec.Header().Get("Location"), "/vehicles/")
}

func documentForm(c *testClient, fields map[string]string) url.Values {
	form := url.Values{
		"csrf_token": {c.csrfFrom(c.get("/documents/new"))},
		"doc_date":   {businessToday(time.Now()).Format("2006-01-02")},
	}
	for k, v := range fields {
		form.Set(k, v)
	}
	return form
}

func TestCashSaleFlowAndArchivedPDF(t *testing.T) {
	fake := newFakePDF()
	h, clock := newTestServerWithPDF(t, fake)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	customerID := createCustomerHTTP(t, c, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, c, "ZAPM0000000001", "1,250,000")

	if rec := c.get("/documents/new"); rec.Code != http.StatusOK {
		t.Fatalf("GET /documents/new = %d:\n%s", rec.Code, rec.Body.String())
	}
	if rec := c.get("/documents"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No documents match") {
		t.Fatalf("empty document list = %d:\n%s", rec.Code, rec.Body.String())
	}

	form := documentForm(c, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
		"down":        "1",
	})
	form.Set("doc_date", businessToday(clock.Now()).Format("2006-01-02"))
	rec := c.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /documents = %d:\n%s", rec.Code, rec.Body.String())
	}
	docID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")

	rec = c.get("/documents/" + docID + "?created=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET document = %d:\n%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"Cash sale 0001", "Saman Perera", "1,250,000.00", "ISSUED", "Document issued"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("document page missing %q", want)
		}
	}

	rec = c.get("/documents")
	for _, want := range []string{"0001", "Cash sale", "Saman Perera"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("document list missing %q", want)
		}
	}

	// The unit is sold and the ledger agrees.
	if body := c.get("/vehicles/" + vehicleID).Body.String(); !strings.Contains(body, "SOLD") {
		t.Error("vehicle page does not show SOLD")
	}
	if body := c.get("/stock").Body.String(); !strings.Contains(body, "Sold") {
		t.Errorf("stock page does not show the sale:\n%s", body)
	}

	// First print renders and archives; the second serves the same bytes
	// without calling the renderer again.
	rec = c.get("/documents/" + docID + "/pdf")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET pdf = %d:\n%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("pdf content type = %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "%PDF") {
		t.Fatal("pdf body does not look like a pdf")
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "sales_cash-0001.pdf") {
		t.Errorf("content disposition = %q", cd)
	}
	first := append([]byte(nil), rec.Body.Bytes()...)
	rec = c.get("/documents/" + docID + "/pdf")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), first) {
		t.Fatal("second download is not the archived bytes")
	}
	if fake.Calls() != 1 {
		t.Fatalf("renderer called %d times, want 1", fake.Calls())
	}

	audit := c.get("/audit").Body.String()
	for _, want := range []string{"document.create", "stock.sell", "document.pdf"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit missing %q", want)
		}
	}

	// The same unit cannot be sold again.
	form = documentForm(c, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
	})
	rec = c.post("/documents", form)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second sale = %d, want 409:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no longer available") {
		t.Fatalf("second sale body:\n%s", rec.Body.String())
	}
}

func TestQuotationConvertsAndSalesSeesOnlyOwn(t *testing.T) {
	fake := newFakePDF()
	h, clock := newTestServerWithPDF(t, fake)
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
	ownerCustomer := createCustomerHTTP(t, owner, "Owner Customer", "900000000001")
	firstVehicle := createVehicleHTTP(t, owner, "ZAPM0000000101", "1,250,000")
	secondVehicle := createVehicleHTTP(t, owner, "ZAPM0000000102", "1,250,000")

	form := documentForm(owner, map[string]string{
		"doc_type":    store.DocQuotation,
		"customer_id": ownerCustomer,
		"vehicle_id":  firstVehicle,
		"price":       "1,250,000",
	})
	form.Set("doc_date", businessToday(clock.Now()).Format("2006-01-02"))
	rec := owner.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create quotation = %d:\n%s", rec.Code, rec.Body.String())
	}
	quoteID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")

	if body := owner.get("/documents/" + quoteID).Body.String(); !strings.Contains(body, "OPEN") || !strings.Contains(body, "Create invoice from quotation") {
		t.Fatalf("quotation page:\n%s", body)
	}

	if rec := owner.get("/documents/new?quote_id=" + quoteID); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "New invoice from quotation 0001") {
		t.Fatalf("convert page = %d:\n%s", rec.Code, rec.Body.String())
	}

	form = documentForm(owner, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": ownerCustomer,
		"vehicle_id":  firstVehicle,
		"price":       "1,250,000",
		"quote_id":    quoteID,
	})
	rec = owner.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("convert = %d:\n%s", rec.Code, rec.Body.String())
	}
	invoiceID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")
	if body := owner.get("/documents/" + quoteID).Body.String(); !strings.Contains(body, "CONVERTED") {
		t.Fatalf("quotation was not marked converted:\n%s", body)
	}

	// A closed quotation cannot be used for a second invoice, even on a
	// different unit.
	form = documentForm(owner, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": ownerCustomer,
		"vehicle_id":  secondVehicle,
		"price":       "1,250,000",
		"quote_id":    quoteID,
	})
	rec = owner.post("/documents", form)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already been used") {
		t.Fatalf("second conversion = %d:\n%s", rec.Code, rec.Body.String())
	}

	sales := signIn(t, h, "alpha.local", "sales@alpha.local", password)

	// Sales cannot see the owner's invoice or print it.
	if rec := sales.get("/documents/" + invoiceID); rec.Code != http.StatusNotFound {
		t.Fatalf("sales read owner invoice = %d, want 404", rec.Code)
	}
	if rec := sales.get("/documents/" + invoiceID + "/pdf"); rec.Code != http.StatusNotFound {
		t.Fatalf("sales printed owner invoice = %d, want 404", rec.Code)
	}
	if body := sales.get("/documents").Body.String(); strings.Contains(body, "Owner Customer") {
		t.Fatalf("sales list leaked the owner's document:\n%s", body)
	}

	// Sales can quote, and only sees their own quote.
	salesCustomer := createCustomerHTTP(t, sales, "Sales Customer", "900000000002")
	form = documentForm(sales, map[string]string{
		"doc_type":    store.DocQuotation,
		"customer_id": salesCustomer,
		"vehicle_id":  secondVehicle,
		"price":       "1,200,000",
	})
	rec = sales.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sales quotation = %d:\n%s", rec.Code, rec.Body.String())
	}
	salesDocID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")
	body := sales.get("/documents").Body.String()
	if !strings.Contains(body, "Sales Customer") {
		t.Errorf("sales list missing own document:\n%s", body)
	}
	if strings.Contains(body, "Owner Customer") {
		t.Errorf("sales list includes the owner's document:\n%s", body)
	}
	if rec := sales.get("/documents/" + salesDocID + "/pdf"); rec.Code != http.StatusOK {
		t.Errorf("sales cannot print own quote = %d", rec.Code)
	}

	// The owner sees both.
	if body := owner.get("/documents").Body.String(); !strings.Contains(body, "Owner Customer") || !strings.Contains(body, "Sales Customer") {
		t.Errorf("owner list is missing a document:\n%s", body)
	}
}

func TestDocumentValidationAndUnavailablePrinter(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	customerID := createCustomerHTTP(t, c, "Saman Perera", "907654321V")
	vehicleID := createVehicleHTTP(t, c, "ZAPM0000000201", "1,250,000")

	// A proforma must be on Singer paper.
	form := documentForm(c, map[string]string{
		"doc_type":        store.DocProforma,
		"customer_id":     customerID,
		"vehicle_id":      vehicleID,
		"price":           "1,250,000",
		"down":            "250,000",
		"finance_company": "VALLIBEL FINANCE",
	})
	rec := c.post("/documents", form)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Singer Finance paper") {
		t.Fatalf("vallibel proforma = %d:\n%s", rec.Code, rec.Body.String())
	}

	// No future paper.
	form = documentForm(c, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
	})
	form.Set("doc_date", businessToday(clock.Now()).AddDate(0, 0, 1).Format("2006-01-02"))
	rec = c.post("/documents", form)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "future") {
		t.Fatalf("future document = %d:\n%s", rec.Code, rec.Body.String())
	}

	// An advance of nothing is not a deposit.
	form = documentForm(c, map[string]string{
		"doc_type":    store.DocAdvance,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
		"down":        "0",
	})
	rec = c.post("/documents", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero advance = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}

	// Nothing was issued by any of the rejected forms.
	if body := c.get("/documents").Body.String(); !strings.Contains(body, "No documents match") {
		t.Fatalf("a rejected form created a document:\n%s", body)
	}

	// Without a renderer the detail page does not offer a dead print button,
	// and the route explains itself instead of pretending.
	form = documentForm(c, map[string]string{
		"doc_type":    store.DocSalesCash,
		"customer_id": customerID,
		"vehicle_id":  vehicleID,
		"price":       "1,250,000",
	})
	rec = c.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("cash sale = %d:\n%s", rec.Code, rec.Body.String())
	}
	docID := idFromLocation(t, rec.Header().Get("Location"), "/documents/")
	if body := c.get("/documents/" + docID).Body.String(); strings.Contains(body, "Download PDF") {
		t.Error("detail page offers printing with no renderer configured")
	}
	if rec := c.get("/documents/" + docID + "/pdf"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("pdf without renderer = %d, want 503", rec.Code)
	}
}
