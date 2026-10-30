package pdf

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

func sampleProforma() store.Document {
	yom := int32(2026)
	return store.Document{
		ID:               uuid.New(),
		DocType:          store.DocProforma,
		DocNo:            "0001",
		DocDate:          time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
		Status:           store.DocIssued,
		CustomerName:     "MS P.B SEWWANDI",
		CustomerNIC:      "200666602786",
		CustomerPhone:    "0788585327",
		CustomerAddress:  "Mahiyanganaya",
		VehicleMake:      "TAILG",
		VehicleModel:     "F55",
		VehicleType:      "ELECTRIC",
		VehicleColour:    "RED",
		VehicleEngineNo:  "TLLMEVTF5100479",
		VehicleChassisNo: "LGMMVU1K1T0002970",
		VehicleYOM:       &yom,
		FinanceCompany:   "SINGER FINANCE",
		FinanceAddress:   "MAHIYANAGNAYA",
		PriceCents:       98000000,
		DownCents:        43000000,
		BalanceCents:     55000000,
	}
}

func TestProformaCarriesEveryFieldFromTheSample(t *testing.T) {
	html := proformaHTML(sampleProforma())

	for _, want := range []string{
		"PROFORMA INVOICE",
		"Proforma Invoice. No.  0001",
		"DATE:         21/10/2026",
		"Customer Name     - MS P.B SEWWANDI",
		"Customer NIC- 200666602786",
		"Contact no     - 0788585327",
		"Leasing company- SINGER FINANCE",
		"Address- MAHIYANAGNAYA",
		"MAKE                      - TAILG",
		"MODEL                    - F55",
		"TYPE                       - ELECTRIC",
		"COLOUR                  - RED",
		"EN/MOTOR NO       - TLLMEVTF5100479",
		"LGMMVU1K1T0002970",
		"YOM                      - 2026",
		"Selling price         - LKR 980000/-",
		"Cash Advance      -LKR 430000/-",
		"Balance Amount -LKR 550000/-",
		"VAT REG. NO. 124008026-7000",
		"VALIDITY \u2013 07 DAYS",
		"RMV free of Charge",
		"SINGER (SRI LANKA) PLC",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("proforma missing %q", want)
		}
	}
}

func TestProformaKeepsTheSampleGeometry(t *testing.T) {
	html := proformaHTML(sampleProforma())

	for _, want := range []string{
		"@page { size: 612pt 792pt; margin: 0; }",
		"width: 566.5pt",
		"margin-left: -45.25pt",
		"left:433.2pt;top:21.45pt;width:154.45pt;height:24.6pt",
		"width:323.25pt;height:36pt",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("proforma geometry missing %q", want)
		}
	}

	// Every font and image is embedded, so rendering never depends on the
	// network and Gotenberg cannot be pointed at an internal url.
	if strings.Count(html, "data:font/ttf;base64,") != 2 {
		t.Error("the two Carlito faces are not embedded")
	}
	for _, want := range []string{"data:image/svg+xml;base64,", "data:image/jpeg;base64,"} {
		if !strings.Contains(html, want) {
			t.Errorf("asset %q is missing", want)
		}
	}
}

func TestProformaEscapesWhatPeopleType(t *testing.T) {
	d := sampleProforma()
	d.CustomerName = `<script>alert("x")</script>`
	d.Notes = ""
	html := proformaHTML(d)
	if strings.Contains(html, "<script>alert") {
		t.Fatal("customer name was not escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("escaped customer name is missing")
	}
}

func TestDealerDocuments(t *testing.T) {
	base := sampleProforma()
	base.DocType = store.DocSalesCash
	base.DocNo = "0042"
	base.CustomerName = "Nimal & Sons"
	base.PriceCents = 125000000
	base.DownCents = 125000000
	base.BalanceCents = 0
	base.FinanceCompany = ""

	cash := dealerHTML("Gunawardana Enterprises", base)
	for _, want := range []string{
		"SALES INVOICE",
		"No. 0042",
		"Gunawardana Enterprises",
		"Beliatta Road, Tangalle",
		"Nimal &amp; Sons",
		"1,250,000.00",
		"Paid in full",
	} {
		if !strings.Contains(cash, want) {
			t.Errorf("cash invoice missing %q", want)
		}
	}

	quote := base
	quote.DocType = store.DocQuotation
	quote.DownCents = 0
	quote.BalanceCents = 125000000
	html := dealerHTML("Gunawardana Enterprises", quote)
	if !strings.Contains(html, "QUOTATION") || !strings.Contains(html, "valid for 7 days") {
		t.Error("quotation is not marked as a valid-for-7-days quote")
	}

	lease := base
	lease.DocType = store.DocSalesLease
	lease.DownCents = 25000000
	lease.BalanceCents = 100000000
	lease.FinanceCompany = "SINGER FINANCE"
	lease.FinanceAddress = "Mahiyanganaya"
	html = dealerHTML("Gunawardana Enterprises", lease)
	for _, want := range []string{"SALES INVOICE", "SINGER FINANCE", "payable by the finance company", "Cash advance"} {
		if !strings.Contains(html, want) {
			t.Errorf("lease invoice missing %q", want)
		}
	}

	advance := base
	advance.DocType = store.DocAdvance
	advance.DownCents = 10000000
	advance.BalanceCents = 115000000
	html = dealerHTML("Gunawardana Enterprises", advance)
	for _, want := range []string{"ADVANCE RECEIPT", "Advance received", "Received with thanks"} {
		if !strings.Contains(html, want) {
			t.Errorf("advance receipt missing %q", want)
		}
	}
}

func TestRenderNeedsConfiguration(t *testing.T) {
	r := New("", "", "")
	if _, err := r.Render(context.Background(), "Alpha", sampleProforma()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("render with no endpoint = %v, want ErrNotConfigured", err)
	}
}

func TestGotenbergClientSendsOneSelfContainedFile(t *testing.T) {
	var gotAuth string
	var gotField string
	var gotHTML string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/forms/chromium/convert/html" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotField = r.FormValue("printBackground")
		file, header, err := r.FormFile("files")
		if err != nil {
			t.Errorf("form file: %v", err)
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		if header.Filename != "index.html" {
			t.Errorf("filename = %q", header.Filename)
		}
		b, _ := io.ReadAll(file)
		gotHTML = string(b)

		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7 test pdf"))
	}))
	defer srv.Close()

	// Basic auth without a real username fails to build the header, so use a
	// throwaway server with the credentials set.
	r := New(srv.URL, "user", "secret")
	out, err := r.Render(context.Background(), "Alpha", sampleProforma())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if string(out) != "%PDF-1.7 test pdf" {
		t.Fatalf("pdf = %q", out)
	}
	if gotAuth != "Basic dXNlcjpzZWNyZXQ=" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotField != "true" {
		t.Errorf("printBackground = %q", gotField)
	}
	if !strings.HasPrefix(gotHTML, "<!DOCTYPE html>") || !strings.Contains(gotHTML, "PROFORMA INVOICE") {
		t.Errorf("html sent to Gotenberg is wrong: %.80q", gotHTML)
	}
}

func TestGotenbergClientRejectsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "chromium crashed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := New(srv.URL, "", "")
	if _, err := r.Render(context.Background(), "Alpha", sampleProforma()); err == nil {
		t.Fatal("render succeeded against a failing renderer")
	}
}

func TestFilenames(t *testing.T) {
	d := sampleProforma()
	if got := Filename(d); got != "proforma-0001.pdf" {
		t.Errorf("filename = %q", got)
	}
	d.DocType = store.DocSalesCash
	if got := Filename(d); got != "sales_cash-0001.pdf" {
		t.Errorf("filename = %q", got)
	}
}
