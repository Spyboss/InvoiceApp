package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/store"
)

// addUserRole creates a second person in the tenant, because the questions
// worth asking about money screens are about who may open them.
func addUserRole(t *testing.T, tenantID uuid.UUID, email, name, role, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	err = webStore.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		_, err := q.CreateUser(context.Background(), store.CreateUserParams{
			TenantID:     tenantID,
			Email:        email,
			Name:         name,
			PasswordHash: hash,
			Role:         role,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create %s user: %v", role, err)
	}
}

// createLeasingInvoiceHTTP issues an invoice that still owes money, which is
// the only paper a receipt can pay down. The customer's NIC comes from the
// chassis so two invoices on one tenant never collide.
func createLeasingInvoiceHTTP(t *testing.T, c *testClient, clock *testClock, chassis, price, down string) string {
	t.Helper()
	customerID := createCustomerHTTP(t, c, "Saman Perera", "NIC-"+chassis[len(chassis)-4:])
	vehicleID := createVehicleHTTP(t, c, chassis, price)
	return issueLeasingInvoice(t, c, clock, customerID, vehicleID, price, down)
}

// issueLeasingInvoice posts a leasing sale for a customer and a unit that
// already exist. A clerk may do this; receiving the unit into the yard is a
// different permission and a different person.
func issueLeasingInvoice(t *testing.T, c *testClient, clock *testClock, customerID, vehicleID, price, down string) string {
	t.Helper()
	form := documentForm(c, map[string]string{
		"doc_type":        store.DocSalesLease,
		"customer_id":     customerID,
		"vehicle_id":      vehicleID,
		"price":           price,
		"down":            down,
		"finance_company": "SINGER FINANCE",
	})
	form.Set("doc_date", businessToday(clock.Now()).Format("2006-01-02"))
	rec := c.post("/documents", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("leasing sale = %d:\n%s", rec.Code, rec.Body.String())
	}
	return idFromLocation(t, rec.Header().Get("Location"), "/documents/")
}

// payInvoice takes a payment the way the browser does: open the form, read the
// token out of the page, post it back.
func payInvoice(t *testing.T, c *testClient, documentID, amount, method, reference string) *httptest.ResponseRecorder {
	t.Helper()
	form := c.get("/documents/" + documentID + "/payment")
	if form.Code != http.StatusOK {
		t.Fatalf("GET payment form = %d:\n%s", form.Code, form.Body.String())
	}
	return c.post("/payments", url.Values{
		"csrf_token":  {c.csrfFrom(form)},
		"document_id": {documentID},
		"amount":      {amount},
		"method":      {method},
		"reference":   {reference},
	})
}

func TestPaymentFlowAndDayEnd(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// 1,000,000.00 sold, 400,000.00 down, so 600,000.00 is owed.
	docID := createLeasingInvoiceHTTP(t, c, clock, "ZAPM0000005001", "1,000,000", "400,000")

	page := c.get("/documents/" + docID).Body.String()
	for _, want := range []string{"Take a payment", "Balance at issue", "600,000.00", "No money has been taken"} {
		if !strings.Contains(page, want) {
			t.Fatalf("invoice page missing %q:\n%s", want, page)
		}
	}

	form := c.get("/documents/" + docID + "/payment")
	if form.Code != http.StatusOK {
		t.Fatalf("GET payment form = %d:\n%s", form.Code, form.Body.String())
	}
	for _, want := range []string{"Receipt for Leasing sale 0001", "600,000.00", "Saman Perera"} {
		if !strings.Contains(form.Body.String(), want) {
			t.Fatalf("payment form missing %q:\n%s", want, form.Body.String())
		}
	}

	rec := c.post("/payments", url.Values{
		"csrf_token":  {c.csrfFrom(form)},
		"document_id": {docID},
		"amount":      {"250,000"},
		"method":      {store.MethodCash},
		"reference":   {"counter"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /payments = %d:\n%s", rec.Code, rec.Body.String())
	}
	receiptID := idFromLocation(t, rec.Header().Get("Location"), "/payments/")

	receipt := c.get("/payments/" + receiptID + "?created=1").Body.String()
	for _, want := range []string{"Receipt 0001", "Receipt recorded", "250,000.00", "Cash", "Saman Perera", "Leasing sale 0001"} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt page missing %q:\n%s", want, receipt)
		}
	}

	// The invoice shows what has been paid and what is left, without changing
	// what was printed on it.
	page = c.get("/documents/" + docID).Body.String()
	for _, want := range []string{"Paid since issue", "250,000.00", "350,000.00", "0001"} {
		if !strings.Contains(page, want) {
			t.Fatalf("invoice page missing %q after payment:\n%s", want, page)
		}
	}

	if list := c.get("/payments").Body.String(); !strings.Contains(list, "0001") || !strings.Contains(list, "250,000.00") {
		t.Fatalf("payments list:\n%s", list)
	}

	// The day so far, before it is closed.
	day := c.get("/day-end").Body.String()
	for _, want := range []string{"Today so far", "Rs 250,000.00", "1 invoice, Rs 1,000,000.00", "Close the day"} {
		if !strings.Contains(day, want) {
			t.Fatalf("day end page missing %q:\n%s", want, day)
		}
	}

	// Float 50,000.00 plus 250,000.00 cash means 300,000.00 expected. The
	// cashier counts 295,000.00, so the drawer is short 5,000.00.
	rec = c.post("/day-end", url.Values{
		"csrf_token":    {c.csrfFrom(c.get("/day-end"))},
		"opening_float": {"50,000"},
		"counted":       {"295,000"},
		"notes":         {"counted twice"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /day-end = %d:\n%s", rec.Code, rec.Body.String())
	}
	zID := idFromLocation(t, rec.Header().Get("Location"), "/day-end/")

	z := c.get("/day-end/" + zID + "?closed=1").Body.String()
	for _, want := range []string{"Day end 0001", "Day closed and stored", "300,000.00", "295,000.00", "-5,000.00",
		"short by Rs 5,000.00", "Payments taken", "Documents issued", "counted twice", "Test Owner"} {
		if !strings.Contains(z, want) {
			t.Fatalf("z page missing %q:\n%s", want, z)
		}
	}

	// The day is closed: the screen shows the z instead of the close form.
	day = c.get("/day-end").Body.String()
	if strings.Contains(day, "Close the day") {
		t.Fatalf("a closed day still offers the close form:\n%s", day)
	}
	if !strings.Contains(day, "Z 0001") {
		t.Fatalf("closed day screen does not show the z:\n%s", day)
	}

	// The same day cannot be closed twice through the browser either; the
	// person is sent to the close that already exists.
	rec = c.post("/day-end", url.Values{
		"csrf_token":    {c.csrfFrom(c.get("/"))},
		"opening_float": {"0"},
		"counted":       {"0"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second close = %d, want a redirect:\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/day-end/"+zID {
		t.Fatalf("second close location = %q, want the existing z", loc)
	}

	csv := c.get("/day-end/" + zID + "/csv")
	if csv.Code != http.StatusOK {
		t.Fatalf("z csv = %d:\n%s", csv.Code, csv.Body.String())
	}
	if ct := csv.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("z csv content type = %q", ct)
	}
	for _, want := range []string{"Business date", "Expected in the drawer", "300000.00", "-5000.00", "Receipts taken"} {
		if !strings.Contains(csv.Body.String(), want) {
			t.Fatalf("z csv missing %q:\n%s", want, csv.Body.String())
		}
	}

	audit := c.get("/audit").Body.String()
	for _, want := range []string{"payment.create", "dayend.close"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit missing %q", want)
		}
	}
}

func TestOverpaymentIsRefusedWithACeiling(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	docID := createLeasingInvoiceHTTP(t, c, clock, "ZAPM0000005101", "1,000,000", "400,000")

	// One cent more than the 600,000.00 owed.
	rec := payInvoice(t, c, docID, "600,000.01", store.MethodCash, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("overpayment = %d, want 409:\n%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"more than the invoice still owes", "600,000.00"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("overpayment page missing %q:\n%s", want, rec.Body.String())
		}
	}

	// The refusal burnt no receipt number and wrote nothing.
	if list := c.get("/payments").Body.String(); !strings.Contains(list, "No receipts match") {
		t.Fatalf("a refused payment was written:\n%s", list)
	}

	// Exactly the balance is accepted, and then the invoice takes no more.
	rec = payInvoice(t, c, docID, "600,000", store.MethodBank, "slip 4412")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("settling payment = %d:\n%s", rec.Code, rec.Body.String())
	}

	page := c.get("/documents/" + docID + "/payment").Body.String()
	if !strings.Contains(page, "paid in full") {
		t.Fatalf("a settled invoice still offers a receipt:\n%s", page)
	}
	settled := c.get("/documents/" + docID).Body.String()
	if !strings.Contains(settled, "Outstanding") || !strings.Contains(settled, "0.00") {
		t.Fatalf("settled invoice does not show a zero outstanding:\n%s", settled)
	}
}

func TestReversalIsDatedTheDayItHappens(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	docID := createLeasingInvoiceHTTP(t, c, clock, "ZAPM0000005201", "1,000,000", "400,000")
	rec := payInvoice(t, c, docID, "250,000", store.MethodCash, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("payment = %d:\n%s", rec.Code, rec.Body.String())
	}
	receiptID := idFromLocation(t, rec.Header().Get("Location"), "/payments/")

	// The money is handed back the next day.
	clock.Advance(24 * time.Hour)
	today := businessToday(clock.Now()).Format("02/01/2006")

	form := c.get("/payments/" + receiptID)
	if !strings.Contains(form.Body.String(), "Reverse this receipt") || !strings.Contains(form.Body.String(), today) {
		t.Fatalf("receipt page does not offer a reversal dated today (%s):\n%s", today, form.Body.String())
	}
	token := c.csrfFrom(form)

	// A reason is required, and the refusal reaches the screen.
	short := c.post("/payments/"+receiptID+"/reverse", url.Values{"csrf_token": {token}, "reason": {"no"}})
	if short.Code != http.StatusBadRequest || !strings.Contains(short.Body.String(), "Three characters at least") {
		t.Fatalf("short reason = %d:\n%s", short.Code, short.Body.String())
	}

	rec = c.post("/payments/"+receiptID+"/reverse", url.Values{
		"csrf_token": {token},
		"reason":     {"customer cancelled the sale"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reverse = %d:\n%s", rec.Code, rec.Body.String())
	}

	page := c.get("/payments/" + receiptID + "?reversed=1").Body.String()
	for _, want := range []string{"REVERSED", "Receipt reversed", "customer cancelled the sale", today} {
		if !strings.Contains(page, want) {
			t.Fatalf("reversed receipt missing %q:\n%s", want, page)
		}
	}

	// The invoice owes the money again.
	if page := c.get("/documents/" + docID).Body.String(); !strings.Contains(page, "600,000.00") {
		t.Fatalf("the reversal did not free the balance:\n%s", page)
	}

	// Today's z carries the money out, not the day it came in.
	day := c.get("/day-end").Body.String()
	for _, want := range []string{"Cash handed back", "Rs 250,000.00"} {
		if !strings.Contains(day, want) {
			t.Fatalf("the reversal is not on today's day end (%q):\n%s", want, day)
		}
	}
}

func TestSalesClerkCannotTouchTheMoney(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	addUserRole(t, tn.ID, "sales@alpha.local", "Sales Person", "SALES", password)

	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	ownerDoc := createLeasingInvoiceHTTP(t, owner, clock, "ZAPM0000005301", "1,000,000", "400,000")
	if rec := payInvoice(t, owner, ownerDoc, "250,000", store.MethodCash, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("owner payment = %d:\n%s", rec.Code, rec.Body.String())
	}

	clerk := signIn(t, h, "alpha.local", "sales@alpha.local", password)
	// The owner receives the second unit. The clerk then sells it, because a
	// clerk may issue an invoice but may not touch the money on it.
	clerkCustomer := createCustomerHTTP(t, owner, "Nimal Silva", "NIC-5302")
	clerkVehicle := createVehicleHTTP(t, owner, "ZAPM0000005302", "800,000")
	clerkDoc := issueLeasingInvoice(t, clerk, clock, clerkCustomer, clerkVehicle, "800,000", "300,000")

	for _, path := range []string{"/payments", "/day-end", "/documents/" + clerkDoc + "/payment"} {
		if rec := clerk.get(path); rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s as a clerk = %d, want 403", path, rec.Code)
		}
	}
	if rec := clerk.post("/payments", url.Values{"amount": {"1"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /payments as a clerk = %d, want 403", rec.Code)
	}
	if rec := clerk.post("/day-end", url.Values{"counted": {"1"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /day-end as a clerk = %d, want 403", rec.Code)
	}

	// The nav does not offer what the account cannot open.
	nav := clerk.get("/").Body.String()
	if strings.Contains(nav, `href="/payments"`) || strings.Contains(nav, `href="/day-end"`) {
		t.Fatalf("the clerk's nav offers money screens:\n%s", nav)
	}

	// The invoice the clerk issued hides the money side: no margin, no
	// receipts, no ceiling.
	page := clerk.get("/documents/" + clerkDoc).Body.String()
	for _, unwanted := range []string{"Paid since issue", "Take a payment", "Receipts"} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("the clerk sees %q on the invoice:\n%s", unwanted, page)
		}
	}

	// A report for a clerk covers that clerk's documents only, even though
	// report.view is granted to the role.
	sales := clerk.get("/reports/sales")
	if sales.Code != http.StatusOK {
		t.Fatalf("sales report as a clerk = %d:\n%s", sales.Code, sales.Body.String())
	}
	body := sales.Body.String()
	if !strings.Contains(body, "You see the invoices you issued") {
		t.Fatalf("the report does not say it is scoped:\n%s", body)
	}
	if !strings.Contains(body, "800,000.00") {
		t.Fatalf("the clerk's own invoice is missing from the report:\n%s", body)
	}
	if strings.Contains(body, "1,000,000.00") {
		t.Fatalf("the clerk sees the owner's invoice:\n%s", body)
	}

	// The owner's report covers both invoices.
	ownerReport := owner.get("/reports/sales").Body.String()
	for _, want := range []string{"1,000,000.00", "800,000.00"} {
		if !strings.Contains(ownerReport, want) {
			t.Fatalf("owner sales report missing %q:\n%s", want, ownerReport)
		}
	}

	// The yard report hides what the dealership paid from a clerk.
	if strings.Contains(clerk.get("/reports/stock").Body.String(), "Capital in the yard") {
		t.Fatal("a clerk sees the cost of the yard")
	}
	if !strings.Contains(owner.get("/reports/stock").Body.String(), "Capital in the yard") {
		t.Fatal("the owner cannot see the cost of the yard")
	}
}

func TestReportCSVExportsAreSafe(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// A customer whose name a spreadsheet would run as a formula.
	customerID := createCustomerHTTP(t, c, `=HYPERLINK("http://evil","click")`, "900000000999")
	vehicleID := createVehicleHTTP(t, c, "ZAPM0000005401", "1,000,000")
	form := documentForm(c, map[string]string{
		"doc_type":        store.DocSalesLease,
		"customer_id":     customerID,
		"vehicle_id":      vehicleID,
		"price":           "1,000,000",
		"down":            "400,000",
		"finance_company": "SINGER FINANCE",
	})
	form.Set("doc_date", businessToday(clock.Now()).Format("2006-01-02"))
	if rec := c.post("/documents", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("leasing sale = %d:\n%s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		path     string
		filename string
		header   string
		want     string
	}{
		{"/reports/sales.csv", "sales-", "Group,Name,Invoices,Gross,Advance,Balance", "600000.00"},
		{"/reports/receivables.csv", "receivables-", "Group,Name,Date,Customer", "600000.00"},
		{"/reports/stock.csv", "stock-", "Group,Name,Units,Cost,Price", "SOLD"},
	}
	for _, tc := range cases {
		rec := c.get(tc.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d:\n%s", tc.path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
			t.Fatalf("%s content type = %q", tc.path, ct)
		}
		disposition := rec.Header().Get("Content-Disposition")
		if !strings.HasPrefix(disposition, `attachment; filename="`) || !strings.Contains(disposition, tc.filename) {
			t.Fatalf("%s disposition = %q", tc.path, disposition)
		}
		body := strings.TrimPrefix(rec.Body.String(), "\ufeff")
		if !strings.Contains(body, tc.header) {
			t.Fatalf("%s header:\n%s", tc.path, body)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s missing %q:\n%s", tc.path, tc.want, body)
		}
	}

	// The formula is quoted as data and prefixed, so a spreadsheet shows it.
	body := strings.TrimPrefix(c.get("/reports/receivables.csv").Body.String(), "\ufeff")
	if !strings.Contains(body, `"'=HYPERLINK(""http://evil"",""click"")"`) {
		t.Fatalf("the formula was not neutralised:\n%s", body)
	}
	// A negative number keeps its sign, so a variance still reads as one.
	if !strings.Contains(body, ",") || strings.Contains(body, "'-") {
		t.Fatalf("numbers were mangled:\n%s", body)
	}
}

func TestReceiptsAndDayEndAreInvisibleToOtherTenants(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	createTenantAndOwner(t, "Beta Motors", "beta.local", "owner@beta.local", password)

	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	docID := createLeasingInvoiceHTTP(t, owner, clock, "ZAPM0000005501", "1,000,000", "400,000")
	rec := payInvoice(t, owner, docID, "250,000", store.MethodCash, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("payment = %d:\n%s", rec.Code, rec.Body.String())
	}
	receiptID := idFromLocation(t, rec.Header().Get("Location"), "/payments/")

	other := signIn(t, h, "beta.local", "owner@beta.local", password)

	if got := other.get("/payments/" + receiptID); got.Code != http.StatusNotFound {
		t.Fatalf("beta read alpha's receipt = %d, want 404", got.Code)
	}
	if got := other.get("/payments"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "No receipts match") {
		t.Fatalf("beta payments list:\n%s", got.Body.String())
	}
	dayEnd := other.get("/day-end")
	if dayEnd.Code != http.StatusOK {
		t.Fatalf("beta day end = %d", dayEnd.Code)
	}
	if strings.Contains(dayEnd.Body.String(), "250,000.00") {
		t.Fatalf("beta's day end shows alpha's money:\n%s", dayEnd.Body.String())
	}
	if got := other.get("/documents/" + docID); got.Code != http.StatusNotFound {
		t.Fatalf("beta read alpha's invoice = %d, want 404", got.Code)
	}
	if got := other.get("/reports/receivables"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "Nothing is owed") {
		t.Fatalf("beta receivables report:\n%s", got.Body.String())
	}
}

func TestMoneyCannotBeTakenAfterTheDayIsClosed(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	docID := createLeasingInvoiceHTTP(t, c, clock, "ZAPM0000005601", "1,000,000", "400,000")
	if rec := payInvoice(t, c, docID, "100,000", store.MethodCash, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("payment = %d:\n%s", rec.Code, rec.Body.String())
	}

	// The cashier counts the drawer and closes the day while the invoice still
	// owes money.
	rec := c.post("/day-end", url.Values{
		"csrf_token":    {c.csrfFrom(c.get("/day-end"))},
		"opening_float": {"0"},
		"counted":       {"100,000"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("close = %d:\n%s", rec.Code, rec.Body.String())
	}

	// The receipt screen says why, in place of the form, so the cashier is not
	// asked to type money that would land on a day already signed off.
	page := c.get("/documents/" + docID + "/payment").Body.String()
	if !strings.Contains(page, "already closed") {
		t.Fatalf("the receipt screen still offers a form after the z:\n%s", page)
	}
	if strings.Contains(page, "Record receipt") {
		t.Fatalf("the closed day still offers the receipt form:\n%s", page)
	}

	// A post that skips the screen is refused, and nothing is written.
	rec = c.post("/payments", url.Values{
		"csrf_token":  {c.csrfFrom(c.get("/"))},
		"document_id": {docID},
		"amount":      {"50,000"},
		"method":      {store.MethodCash},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("payment after the z = %d, want 409:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already closed") {
		t.Fatalf("the refusal does not explain the closed day:\n%s", rec.Body.String())
	}
	if list := c.get("/payments").Body.String(); strings.Contains(list, "50,000.00") {
		t.Fatalf("money was recorded on a closed day:\n%s", list)
	}

	// The invoice still owes what it owed: the closed day is untouched.
	if page := c.get("/documents/" + docID).Body.String(); !strings.Contains(page, "500,000.00") {
		t.Fatalf("the invoice balance moved:\n%s", page)
	}
}
