package store_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/legacy"
	"github.com/spyboss/invoiceapp/internal/store"
)

// testFileSHA is what a run records for the bytes it imported. The tests do not
// hash anything, they only have to look like a real run.
const testFileSHA = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

var invoiceColumns = []string{
	"invoice_type", "invoice_no", "date", "customer", "nic", "cust_addr", "delivery",
	"model", "engine", "chassis", "color", "price", "down", "balance",
	"finance_company", "finance_address", "dealer", "payment_method",
}

// invoiceFile builds the csv the old app wrote and parses it, so a test starts
// from the same place the screen does. Each row is the common case with the
// named cells changed.
func invoiceFile(t *testing.T, rows ...map[string]string) legacy.InvoiceFile {
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

	file, err := legacy.ParseInvoices(&buf)
	if err != nil {
		t.Fatalf("the test file does not parse: %v", err)
	}
	if len(file.Problems) > 0 {
		t.Fatalf("the test file is not clean: %v", file.Problems)
	}
	return file
}

// importInvoices runs one import the way the screen does, in a tenant
// transaction.
func importInvoices(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, in store.InvoiceImport) (store.InvoiceImportResult, error) {
	t.Helper()
	var res store.InvoiceImportResult
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		res, err = q.ImportInvoices(context.Background(), tenantID, actorID, in)
		return err
	})
	return res, err
}

func importStock(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, in store.StockImport) (store.StockImportResult, error) {
	t.Helper()
	var res store.StockImportResult
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		res, err = q.ImportStock(context.Background(), tenantID, actorID, in)
		return err
	})
	return res, err
}

// documentsByNo reads every document back, keyed by its type and number.
func documentsByNo(t *testing.T, st *store.Store, tenantID uuid.UUID) map[string]store.Document {
	t.Helper()
	out := make(map[string]store.Document)
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		docs, err := q.ListDocuments(context.Background(), tenantID, store.DocumentFilter{Limit: 200})
		if err != nil {
			return err
		}
		for _, d := range docs {
			out[d.DocType+"-"+d.DocNo] = d
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list documents: %v", err)
	}
	return out
}

func vehicleStatus(t *testing.T, st *store.Store, tenantID uuid.UUID, chassis string) string {
	t.Helper()
	var status string
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		vehicles, err := q.ListVehicles(context.Background(), tenantID, "", chassis, 5)
		if err != nil {
			return err
		}
		if len(vehicles) != 1 {
			return fmt.Errorf("vehicles for %s = %d, want 1", chassis, len(vehicles))
		}
		status = vehicles[0].Status
		return nil
	})
	if err != nil {
		t.Fatalf("read vehicle %s: %v", chassis, err)
	}
	return status
}

func stockSummary(t *testing.T, st *store.Store, tenantID uuid.UUID) store.StockSummary {
	t.Helper()
	var summary store.StockSummary
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		summary, err = q.StockSummaryFor(context.Background(), tenantID)
		return err
	})
	if err != nil {
		t.Fatalf("stock summary: %v", err)
	}
	return summary
}

// TestImportInvoicesBringsTheBookIn is the acceptance test of step 7 at the
// store: the papers the old app issued are written with their own numbers,
// their own money, and the stock effects the papers describe.
func TestImportInvoicesBringsTheBookIn(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := invoiceFile(t,
		map[string]string{"invoice_no": "0007"},
		map[string]string{
			"invoice_no": "0008", "customer": "Nimal Silva", "nic": "881234567V",
			"chassis": "ZAPM0000000002", "engine": "ENG-2",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
		map[string]string{
			"invoice_type": "ADVANCE", "invoice_no": "0002", "chassis": "ZAPM0000000003",
			"engine": "ENG-3", "down": "100000.0", "balance": "1150000.0",
		},
		map[string]string{
			"invoice_type": "PROFORMA", "invoice_no": "0003", "chassis": "ZAPM0000000004",
			"engine": "ENG-4", "down": "200000.0", "balance": "1050000.0",
		},
	)

	res, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: file,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 4 || res.Skipped != 0 {
		t.Fatalf("imported %d skipped %d, want 4 and 0", res.Imported, res.Skipped)
	}
	if res.Totals != file.Totals {
		t.Fatalf("totals = %+v, want the file's %+v", res.Totals, file.Totals)
	}
	if res.RunID == uuid.Nil {
		t.Fatal("the run has no id")
	}

	docs := documentsByNo(t, st, alpha.ID)
	for _, want := range []string{"SALES_CASH-0007", "SALES_LEASING-0008", "ADVANCE-0002", "PROFORMA-0003"} {
		if _, ok := docs[want]; !ok {
			t.Fatalf("%s is not in the book; got %v", want, keysOf(docs))
		}
	}

	cash := docs["SALES_CASH-0007"]
	if cash.DownCents != cash.PriceCents || cash.BalanceCents != 0 {
		t.Errorf("a cash sale imported with down %d balance %d", cash.DownCents, cash.BalanceCents)
	}
	if cash.CustomerName != "Saman Perera" || cash.VehicleChassisNo != "ZAPM0000000001" {
		t.Errorf("the paper's own words were not kept: %+v", cash)
	}
	if cash.DocYear != 2026 || cash.DocDate.Format("2006-01-02") != "2026-10-30" {
		t.Errorf("date/year = %v/%d", cash.DocDate, cash.DocYear)
	}

	lease := docs["SALES_LEASING-0008"]
	if lease.BalanceCents != 100000000 {
		t.Errorf("leasing balance = %d, want 100000000", lease.BalanceCents)
	}
	if lease.FinanceCompany != "Vallibel Finance PLC" {
		t.Errorf("finance company = %q", lease.FinanceCompany)
	}

	for chassis, want := range map[string]string{
		"ZAPM0000000001": store.StatusSold,
		"ZAPM0000000002": store.StatusSold,
		"ZAPM0000000003": store.StatusReserved,
		"ZAPM0000000004": store.StatusAvailable,
	} {
		if got := vehicleStatus(t, st, alpha.ID, chassis); got != want {
			t.Errorf("chassis %s is %s, want %s", chassis, got, want)
		}
	}

	// Two units left the yard and two never did, and the ledger agrees.
	summary := stockSummary(t, st, alpha.ID)
	if summary.Sold != 2 || summary.Reserved != 1 || summary.Available != 1 {
		t.Errorf("summary = %+v, want 2 sold, 1 reserved, 1 available", summary)
	}
	if summary.LedgerTotal != 2 {
		t.Errorf("ledger total = %d, want 2", summary.LedgerTotal)
	}

	// The next paper this app issues continues above the paper already on a
	// customer's desk. Cash and leasing share one printed number space, so
	// both sit above the higher of the two imported numbers.
	var nextCash, nextLease string
	err = st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		var err error
		if nextCash, err = q.AllocateNo(context.Background(), alpha.ID, store.DocSalesCash, 2026); err != nil {
			return err
		}
		nextLease, err = q.AllocateNo(context.Background(), alpha.ID, store.DocSalesLease, 2026)
		return err
	})
	if err != nil {
		t.Fatalf("allocate after import: %v", err)
	}
	if nextCash != "0009" {
		t.Errorf("next cash number = %q, want 0009 (above every imported number)", nextCash)
	}
	if nextLease != "0009" {
		t.Errorf("next leasing number = %q, want 0009", nextLease)
	}

	runs, err := listImportRuns(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("import runs: %v", err)
	}
	if len(runs) != 1 || runs[0].RowsImported != 4 || runs[0].Kind != store.ImportInvoices {
		t.Fatalf("runs = %+v", runs)
	}
}

// TestImportInvoicesTwiceChangesNothing is what makes the parallel month
// workable: the same file is uploaded every day, and only the new papers land.
func TestImportInvoicesTwiceChangesNothing(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := invoiceFile(t,
		map[string]string{"invoice_no": "0031"},
		map[string]string{
			"invoice_no": "0032", "chassis": "ZAPM0000000012", "engine": "ENG-12",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
	)
	in := store.InvoiceImport{Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: file}

	first, err := importInvoices(t, st, alpha.ID, owner.ID, in)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if first.Imported != 2 {
		t.Fatalf("first import wrote %d papers, want 2", first.Imported)
	}
	before := stockSummary(t, st, alpha.ID)

	second, err := importInvoices(t, st, alpha.ID, owner.ID, in)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Imported != 0 || second.Skipped != 2 {
		t.Fatalf("second import wrote %d and skipped %d, want 0 and 2", second.Imported, second.Skipped)
	}
	if second.Totals != first.Totals {
		t.Fatalf("second totals = %+v, want %+v", second.Totals, first.Totals)
	}

	if docs := documentsByNo(t, st, alpha.ID); len(docs) != 2 {
		t.Fatalf("%d documents after importing twice, want 2: %v", len(docs), keysOf(docs))
	}
	if after := stockSummary(t, st, alpha.ID); after != before {
		t.Fatalf("stock changed on the second import: %+v then %+v", before, after)
	}
}

// TestImportInvoicesRefusesAFileThatDisagreesWithTheBook: a number already in
// the book with other money is not overwritten and not accepted quietly.
func TestImportInvoicesRefusesAFileThatDisagreesWithTheBook(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	original := invoiceFile(t, map[string]string{"invoice_no": "0040"})
	if _, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: original,
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	changed := invoiceFile(t,
		map[string]string{"invoice_no": "0040", "price": "1300000.0", "down": "1300000.0"},
		map[string]string{"invoice_no": "0041", "chassis": "ZAPM0000000041", "engine": "ENG-41"},
	)
	_, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: changed,
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("import of a disagreeing file = %v, want a refusal", err)
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T", err)
	}
	if len(refusal.Problems) != 1 || !strings.Contains(refusal.Problems[0].Problem, "1,300,000.00") {
		t.Fatalf("problems = %+v", refusal.Problems)
	}

	// Nothing from the refused file was written, not even the row that agreed.
	if docs := documentsByNo(t, st, alpha.ID); len(docs) != 1 {
		t.Fatalf("%d documents after a refused import, want 1: %v", len(docs), keysOf(docs))
	}
}

// TestImportInvoicesRefusesPapersTheBookCannotHold collects the rows a
// dealership really sends, each one wrong in a different way.
func TestImportInvoicesRefusesPapersTheBookCannotHold(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := invoiceFile(t,
		// The price and the two payments do not add up.
		map[string]string{"invoice_no": "0050", "price": "1250000.0", "down": "250000.0", "balance": "900000.0"},
		// A proforma for a unit that is already paid for in full, which is a
		// sale and not something a leasing company settles.
		map[string]string{
			"invoice_type": "PROFORMA", "invoice_no": "0051", "chassis": "ZAPM0000000051",
			"price": "1250000.0", "down": "1250000.0", "balance": "0.0",
		},
		// A leasing sale with no finance company.
		map[string]string{
			"invoice_no": "0052", "chassis": "ZAPM0000000052", "finance_company": "",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
		// An advance receipt that records no deposit.
		map[string]string{
			"invoice_type": "ADVANCE", "invoice_no": "0005", "chassis": "ZAPM0000000053",
			"price": "1250000.0", "down": "0.0", "balance": "1250000.0",
		},
	)

	_, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: file,
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("import = %v, want a refusal", err)
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T", err)
	}
	for i, want := range []string{"does not add up", "nothing for the finance company to pay", "no finance company", "records no deposit"} {
		if i >= len(refusal.Problems) || !strings.Contains(refusal.Problems[i].Problem, want) {
			t.Errorf("problem %d = %q, want it to mention %q", i, problemText(refusal, i), want)
		}
	}
	if docs := documentsByNo(t, st, alpha.ID); len(docs) != 0 {
		t.Fatalf("a refused file wrote %d documents", len(docs))
	}
}

// TestImportInvoicesRefusesAPaperThatChangedItsShape: the old app's number is
// what identifies a paper, so a row whose date or money was corrected in the
// export is the same paper disagreeing with the book. It must never land as a
// second paper printing a number the first one already carries.
func TestImportInvoicesRefusesAPaperThatChangedItsShape(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	original := invoiceFile(t, map[string]string{"invoice_no": "0070", "date": "2026-10-30"})
	if _, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: original,
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	// The same number, moved to another year: the row is the same paper, and
	// the row that says otherwise is refused and named.
	movedYear := invoiceFile(t, map[string]string{"invoice_no": "0070", "date": "2025-12-30"})
	_, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: movedYear,
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("import of a paper with a moved date = %v, want a refusal", err)
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T", err)
	}
	if got := problemText(refusal, 0); !strings.Contains(got, "2025-12-30") || !strings.Contains(got, "2026-10-30") {
		t.Fatalf("the refusal does not name both dates: %q", got)
	}

	// And the same number with money still owing, which is a different paper.
	turnedLeasing := invoiceFile(t, map[string]string{
		"invoice_no": "0070", "date": "2026-10-30", "down": "250000.0", "balance": "1000000.0",
	})
	_, err = importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: turnedLeasing,
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("import of a paper that turned into a leasing sale = %v, want a refusal", err)
	}
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T", err)
	}
	if got := problemText(refusal, 0); !strings.Contains(got, "leasing sale") {
		t.Fatalf("the refusal does not say what the paper now reads as: %q", got)
	}

	// One paper, still a cash invoice: nothing was written twice.
	docs := documentsByNo(t, st, alpha.ID)
	if len(docs) != 1 {
		t.Fatalf("%d documents after two refused imports, want 1: %v", len(docs), keysOf(docs))
	}
	for _, d := range docs {
		if d.DocType != store.DocSalesCash {
			t.Fatalf("%s is now a %s", d.DocNo, d.DocType)
		}
	}
}

// TestImportInvoicesRefusesSellingOneUnitTwice: a second sale of a chassis is
// how a book starts lying about the yard.
func TestImportInvoicesRefusesSellingOneUnitTwice(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := invoiceFile(t,
		map[string]string{"invoice_no": "0060"},
		map[string]string{"invoice_no": "0061", "customer": "Nimal Silva", "nic": "881234567V"},
	)
	_, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: file,
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("import = %v, want a refusal", err)
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T", err)
	}
	if len(refusal.Problems) != 1 || !strings.Contains(refusal.Problems[0].Problem, "cannot be sold twice") {
		t.Fatalf("problems = %+v", refusal.Problems)
	}
	if refusal.Problems[0].Line != 3 {
		t.Errorf("the problem is on line %d, want 3", refusal.Problems[0].Line)
	}
	if docs := documentsByNo(t, st, alpha.ID); len(docs) != 0 {
		t.Fatalf("a refused file wrote %d documents", len(docs))
	}
}

// TestImportInvoicesNeedsAMakeForUnitsTheFileDoesNotName: the old invoices file
// carries a model and no make, and the vehicle row needs one.
func TestImportInvoicesNeedsAMakeForUnitsTheFileDoesNotName(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	_, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: " ", File: invoiceFile(t),
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("import = %v, want a refusal", err)
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T", err)
	}
	if len(refusal.Problems) != 1 || !strings.Contains(refusal.Problems[0].Problem, "which make") {
		t.Fatalf("problems = %+v", refusal.Problems)
	}
}

// TestImportRunReadsItsOwnTotalsBack: the page after an import shows what the
// run recorded rather than what the handler remembered, so both totals blobs
// have to decode into the shape the screen reads.
func TestImportRunReadsItsOwnTotalsBack(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := invoiceFile(t,
		map[string]string{"invoice_no": "0100"},
		map[string]string{
			"invoice_no": "0101", "chassis": "ZAPM0000000101", "engine": "ENG-101",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
	)
	res := importRequired(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: file,
	})

	var detail store.ImportRunDetail
	if err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		var err error
		detail, err = q.ImportRunByID(context.Background(), alpha.ID, res.RunID)
		return err
	}); err != nil {
		t.Fatalf("read the run back: %v", err)
	}

	if detail.Kind != store.ImportInvoices || detail.Filename != "invoices.csv" {
		t.Errorf("run = %+v", detail.ImportRun)
	}
	if detail.Source != detail.Stored {
		t.Fatalf("the file says %+v and the book holds %+v", detail.Source, detail.Stored)
	}
	want := store.ImportNumbers{
		Count:        2,
		ValueCents:   250000000,
		DownCents:    150000000,
		BalanceCents: 100000000,
	}
	if detail.Source != want {
		t.Errorf("totals = %+v, want %+v", detail.Source, want)
	}
	if detail.ActorName == nil || *detail.ActorName != "Test User" {
		t.Errorf("actor = %v", detail.ActorName)
	}

	// A run that does not exist is not found rather than a zero row.
	if _, err := func() (store.ImportRunDetail, error) {
		var out store.ImportRunDetail
		err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
			var innerErr error
			out, innerErr = q.ImportRunByID(context.Background(), alpha.ID, uuid.New())
			return innerErr
		})
		return out, err
	}(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a run that does not exist = %v, want not found", err)
	}
}

func problemText(r *store.ImportRefusal, i int) string {
	if i >= len(r.Problems) {
		return ""
	}
	return r.Problems[i].Problem
}

func keysOf(m map[string]store.Document) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func listImportRuns(t *testing.T, st *store.Store, tenantID uuid.UUID) ([]store.ImportRun, error) {
	t.Helper()
	var runs []store.ImportRun
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		runs, err = q.ListImportRuns(context.Background(), tenantID, 20)
		return err
	})
	return runs, err
}
