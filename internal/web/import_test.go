package web

import (
	"bytes"
	"encoding/csv"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spyboss/invoiceapp/internal/legacy"
)

// upload posts a file the way a browser does: a multipart body with the csrf
// token and the typed fields beside it. Nothing in the app could upload a file
// until the csrf middleware learned to read this body, so the helper is the
// only way these screens are ever exercised.
func (c *testClient) upload(path, field, filename string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	return c.uploadFrom(uploadFormPath(path), path, field, filename, data, fields)
}

// uploadFrom posts from a named page, for the case where the person is not
// allowed to open the page the form lives on: the token comes from a page they
// can see, and the refusal has to come from the permission rather than from a
// form they never loaded.
func (c *testClient) uploadFrom(page, path, field, filename string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	form := map[string]string{"csrf_token": c.csrfFrom(c.get(page))}
	for name, value := range fields {
		form[name] = value
	}
	for name, value := range form {
		if err := w.WriteField(name, value); err != nil {
			c.t.Fatalf("write field %s: %v", name, err)
		}
	}
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		c.t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		c.t.Fatalf("write file part: %v", err)
	}
	if err := w.Close(); err != nil {
		c.t.Fatalf("close multipart body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Host = c.host
	req.Header.Set("Content-Type", w.FormDataContentType())
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

// uploadFormPath is the page an upload's token comes from: the invoice and
// stock uploads are forms on one page, the reconciliation is its own.
func uploadFormPath(postPath string) string {
	if strings.HasPrefix(postPath, "/import/") {
		return "/import"
	}
	return postPath
}

// invoiceColumns is the header the old app wrote, in its own order.
var invoiceColumns = []string{
	"invoice_type", "invoice_no", "date", "customer", "nic", "cust_addr", "delivery",
	"model", "engine", "chassis", "color", "price", "down", "balance",
	"finance_company", "finance_address", "dealer", "payment_method",
}

// invoicesCSV writes one invoices.csv: the common cash sale, with the named
// cells changed per row. Prices are written the way the old app wrote them,
// with a trailing .0, because the importer has to survive that too.
func invoicesCSV(t *testing.T, rows ...map[string]string) []byte {
	t.Helper()

	base := map[string]string{
		"invoice_type":    "SALES",
		"invoice_no":      "0001",
		"date":            "2026-10-30",
		"customer":        "Saman Perera",
		"nic":             "907654321V",
		"cust_addr":       "Beliatta Road, Tangalle",
		"model":           "APE City",
		"engine":          "ENG-1",
		"chassis":         "ZAPM0000000001",
		"color":           "White",
		"price":           "1250000.0",
		"down":            "1250000.0",
		"balance":         "0.0",
		"finance_company": "Vallibel Finance PLC",
		"finance_address": "No. 54, Tangalle",
		"dealer":          "Gunawardhana Enterprises",
		"payment_method":  "Cash",
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(invoiceColumns); err != nil {
		t.Fatalf("write header: %v", err)
	}
	for _, overrides := range rows {
		fields := make(map[string]string, len(base))
		for k, v := range base {
			fields[k] = v
		}
		for k, v := range overrides {
			fields[k] = v
		}
		record := make([]string, len(invoiceColumns))
		for i, name := range invoiceColumns {
			record[i] = fields[name]
		}
		if err := w.Write(record); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
	w.Flush()
	return buf.Bytes()
}

// importPageBody is what the redirected page says about the run, so a test can
// assert the sentence a person reads rather than the shape of the html.
func importPage(t *testing.T, c *testClient, location string) string {
	t.Helper()
	rec := c.get(location)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200:\n%s", location, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestImportAndReconciliationAreForThePeopleWhoRunTheBooks(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tenant := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	addUserRole(t, tenant.ID, "sales@alpha.local", "Sales Person", "SALES", password)
	addUserRole(t, tenant.ID, "accounts@alpha.local", "Accounts Person", "ACCOUNTS", password)

	owner := signIn(t, h, "alpha.local", "owner@alpha.local", password)
	sales := signIn(t, h, "alpha.local", "sales@alpha.local", password)
	accounts := signIn(t, h, "alpha.local", "accounts@alpha.local", password)

	for _, path := range []string{"/import", "/reconciliation"} {
		if rec := owner.get(path); rec.Code != http.StatusOK {
			t.Fatalf("owner GET %s = %d, want 200", path, rec.Code)
		}
	}

	// A salesperson may not rewrite the book or read the whole of it.
	for _, path := range []string{"/import", "/reconciliation", "/import/template"} {
		if rec := sales.get(path); rec.Code != http.StatusForbidden {
			t.Errorf("sales GET %s = %d, want 403", path, rec.Code)
		}
	}
	if rec := sales.uploadFrom("/", "/import/invoices", "file", "invoices.csv", invoicesCSV(t, map[string]string{}), nil); rec.Code != http.StatusForbidden {
		t.Errorf("sales POST /import/invoices = %d, want 403", rec.Code)
	}

	// Accounts runs the daily check but does not import.
	if rec := accounts.get("/reconciliation"); rec.Code != http.StatusOK {
		t.Errorf("accounts GET /reconciliation = %d, want 200", rec.Code)
	}
	if rec := accounts.get("/import"); rec.Code != http.StatusForbidden {
		t.Errorf("accounts GET /import = %d, want 403", rec.Code)
	}
}

func TestImportInvoicesUploadWritesTheBook(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// The page offers the two files and nothing has been imported yet.
	body := importPage(t, c, "/import")
	if !strings.Contains(body, "No file has been imported yet") {
		t.Errorf("a fresh import page should say nothing has been imported:\n%s", body)
	}

	file := invoicesCSV(t,
		map[string]string{"invoice_no": "0007"},
		map[string]string{
			"invoice_no": "0008", "customer": "Nimal Silva", "nic": "881234567V",
			"chassis": "ZAPM0000000002", "engine": "ENG-2",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
			"date": businessToday(clock.Now()).Format("2006-01-02"),
		},
	)

	rec := c.upload("/import/invoices", "file", "invoices.csv", file, map[string]string{"default_make": "Piaggio"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /import/invoices = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/import?run=") {
		t.Fatalf("location = %q, want the run that was recorded", location)
	}

	// The page reads the run back from the database, so the totals it shows
	// are the ones the run was accepted on.
	body = importPage(t, c, location)
	for _, want := range []string{
		"<dt>Written</dt><dd>2</dd>",
		"<dt>Already there</dt><dd>0</dd>",
		"<td>2</td><td>2,500,000.00</td><td>1,500,000.00</td><td>1,000,000.00</td>",
		"invoices file",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the run page is missing %q:\n%s", want, body)
		}
	}

	// The imported papers are ordinary documents from here on, marked as
	// imported and printing the number the old app gave them.
	docs := c.get("/documents")
	if docs.Code != http.StatusOK {
		t.Fatalf("GET /documents = %d", docs.Code)
	}
	for _, want := range []string{"0007", "0008"} {
		if !strings.Contains(docs.Body.String(), want) {
			t.Errorf("the imported paper %s is not in the book", want)
		}
	}

	// Uploading the same file again changes nothing: this is what makes the
	// same file safe to send every day of the parallel month.
	rec = c.upload("/import/invoices", "file", "invoices.csv", file, map[string]string{"default_make": "Piaggio"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second upload = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	body = importPage(t, c, rec.Header().Get("Location"))
	for _, want := range []string{"<dt>Written</dt><dd>0</dd>", "<dt>Already there</dt><dd>2</dd>"} {
		if !strings.Contains(body, want) {
			t.Errorf("a repeated import is not reported as a no-op, missing %q:\n%s", want, body)
		}
	}
}

func TestImportInvoicesRefusesTheWholeFileAndSaysWhy(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// The second row's money does not add up, and both rows are refused: a
	// file with one bad row is corrected and uploaded again.
	file := invoicesCSV(t,
		map[string]string{"invoice_no": "0020"},
		map[string]string{
			"invoice_no": "0021", "chassis": "ZAPM0000000021", "engine": "ENG-21",
			"price": "1250000.0", "down": "250000.0", "balance": "900000.0",
		},
	)
	rec := c.upload("/import/invoices", "file", "invoices.csv", file, map[string]string{"default_make": "Piaggio"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST a file that does not add up = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Not imported", "Line 3", "does not add up"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, body)
		}
	}
	// A refused file records no run at all, so the history below stays empty.
	if strings.Contains(body, "<h2>Run</h2>") {
		t.Errorf("a refused file was recorded as a run:\n%s", body)
	}
	// What was typed survives the refusal.
	if !strings.Contains(body, `value="Piaggio"`) {
		t.Errorf("the make was not kept on the refused form:\n%s", body)
	}

	// Nothing landed, so the same number uploads cleanly once the row is fixed.
	fixed := invoicesCSV(t,
		map[string]string{"invoice_no": "0020"},
		map[string]string{
			"invoice_no": "0021", "chassis": "ZAPM0000000021", "engine": "ENG-21",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
	)
	if rec := c.upload("/import/invoices", "file", "invoices.csv", fixed, map[string]string{"default_make": "Piaggio"}); rec.Code != http.StatusSeeOther {
		t.Fatalf("the corrected file = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
}

func TestImportWithoutTheFormTokenIsRefused(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// A multipart body with no token is exactly the upload a hostile page
	// would try, and the middleware has to read the body to see that.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "invoices.csv")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(invoicesCSV(t, map[string]string{})); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := w.WriteField("default_make", "Piaggio"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/import/invoices", &body)
	req.Host = c.host
	req.Header.Set("Content-Type", w.FormDataContentType())
	for name, value := range c.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("an upload with no csrf token = %d, want 403", rec.Code)
	}
}

func TestImportStockTemplateIsTheSheetsOwnHeader(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	rec := c.get("/import/template")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /import/template = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Fatalf("content type = %q", got)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("the template is %d lines, want only the header: %q", len(lines), rec.Body.String())
	}
	header := strings.TrimPrefix(strings.TrimSpace(lines[0]), "\ufeff")
	if header != strings.Join(legacy.StockColumns, ",") {
		t.Fatalf("header = %q", header)
	}
}

func TestImportStockUploadBringsTheYardIn(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	sheet := []byte(strings.Join([]string{
		strings.Join(legacy.StockColumns, ","),
		"S-01,Piaggio,APE City,,,ZAPM0000001001,ENG-1001,White,2026,1050000.00,1250000.00,2026-10-01,AVAILABLE,",
		"S-02,Piaggio,APE City,,,ZAPM0000001002,ENG-1002,White,2026,1050000.00,1250000.00,,RESERVED,Held for a customer",
	}, "\r\n") + "\r\n")

	rec := c.upload("/import/stock", "file", "stock.csv", sheet, map[string]string{
		"received_on": businessToday(clock.Now()).Format("2006-01-02"),
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /import/stock = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	body := importPage(t, c, rec.Header().Get("Location"))
	for _, want := range []string{
		"<dt>Written</dt><dd>2</dd>",
		"<td>2</td><td>2,100,000.00</td><td>2,500,000.00</td>",
		"stock sheet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the stock run page is missing %q:\n%s", want, body)
		}
	}

	vehicles := c.get("/vehicles")
	if vehicles.Code != http.StatusOK {
		t.Fatalf("GET /vehicles = %d", vehicles.Code)
	}
	for _, want := range []string{"ZAPM0000001001", "ZAPM0000001002"} {
		if !strings.Contains(vehicles.Body.String(), want) {
			t.Errorf("the imported unit %s is not in the yard", want)
		}
	}
}

func TestReconciliationChecksTheFileAgainstTheBook(t *testing.T) {
	h, clock := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	today := businessToday(clock.Now())
	file := invoicesCSV(t,
		map[string]string{"invoice_no": "0030", "date": today.Format("2006-01-02")},
		map[string]string{
			"invoice_no": "0031", "date": today.Format("2006-01-02"),
			"chassis": "ZAPM0000000031", "engine": "ENG-31",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
	)
	if rec := c.upload("/import/invoices", "file", "invoices.csv", file, map[string]string{"default_make": "Piaggio"}); rec.Code != http.StatusSeeOther {
		t.Fatalf("import = %d:\n%s", rec.Code, rec.Body.String())
	}

	month := [2]string{today.AddDate(0, 0, -1).Format("2006-01-02"), today.Format("2006-01-02")}
	rec := c.upload("/reconciliation", "file", "invoices.csv", file, map[string]string{
		"date_from": month[0], "date_to": month[1],
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /reconciliation = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	body := importPage(t, c, rec.Header().Get("Location"))
	for _, want := range []string{
		"<dt>Result</dt><dd><span class=\"status\">agrees</span></dd>",
		"<span class=\"status\">agrees</span>",
		"cash invoices",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the reconciled page is missing %q:\n%s", want, body)
		}
	}

	// The next day's file has one more sale in it, which nobody has imported.
	// That is the difference the parallel month is looking for.
	tomorrow := invoicesCSV(t,
		map[string]string{"invoice_no": "0030", "date": today.Format("2006-01-02")},
		map[string]string{
			"invoice_no": "0031", "date": today.Format("2006-01-02"),
			"chassis": "ZAPM0000000031", "engine": "ENG-31",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
		map[string]string{
			"invoice_no": "0032", "date": today.Format("2006-01-02"),
			"chassis": "ZAPM0000000032", "engine": "ENG-32",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
	)
	rec = c.upload("/reconciliation", "file", "invoices.csv", tomorrow, map[string]string{
		"date_from": month[0], "date_to": month[1],
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST a file with an unimported paper = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	body = importPage(t, c, rec.Header().Get("Location"))
	if !strings.Contains(body, "1 fewer paper than the file") {
		t.Errorf("the difference is not on the page:\n%s", body)
	}

	// And the difference is an alert, written in the same transaction, so it
	// reaches the owner even if nobody opens this page again.
	alerts := c.get("/alerts")
	if alerts.Code != http.StatusOK {
		t.Fatalf("GET /alerts = %d", alerts.Code)
	}
	if !strings.Contains(alerts.Body.String(), "Reconciliation") {
		t.Errorf("no reconciliation alert was raised:\n%s", alerts.Body.String())
	}
}

func TestReconciliationRefusesDaysItCannotRead(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)
	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	rec := c.upload("/reconciliation", "file", "invoices.csv", invoicesCSV(t, map[string]string{}), map[string]string{
		"date_from": "31/10/2026", "date_to": "2026-10-31",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST a day that is not a date = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "the first day is not a date") {
		t.Errorf("the refusal does not name the field:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `value="31/10/2026"`) {
		t.Errorf("the typed day was not kept:\n%s", rec.Body.String())
	}

	// The same range the other way round is refused by the store, and the page
	// says so rather than recording a comparison that means nothing.
	rec = c.upload("/reconciliation", "file", "invoices.csv", invoicesCSV(t, map[string]string{}), map[string]string{
		"date_from": "2026-10-31", "date_to": "2026-10-01",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST a backwards range = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "the first day is after the last day") {
		t.Errorf("the refusal does not say what is wrong:\n%s", rec.Body.String())
	}
}
