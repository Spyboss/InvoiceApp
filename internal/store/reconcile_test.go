package store_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/legacy"
	"github.com/spyboss/invoiceapp/internal/store"
)

// stockFile builds the yard sheet and parses it, so a test starts where the
// screen does.
func stockFile(t *testing.T, rows ...map[string]string) legacy.StockFile {
	t.Helper()

	base := map[string]string{
		"stock_no":    "S-01",
		"make":        "Piaggio",
		"model":       "APE City",
		"chassis_no":  "ZAPM0000001001",
		"engine_no":   "ENG-1001",
		"colour":      "White",
		"yom":         "2026",
		"cost":        "1050000.00",
		"price":       "1250000.00",
		"received_on": "2026-10-01",
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(legacy.StockColumns); err != nil {
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
		record := make([]string, len(legacy.StockColumns))
		for i, name := range legacy.StockColumns {
			record[i] = fields[name]
		}
		if err := w.Write(record); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
	w.Flush()

	file, err := legacy.ParseStock(&buf)
	if err != nil {
		t.Fatalf("the test sheet does not parse: %v", err)
	}
	if len(file.Problems) > 0 {
		t.Fatalf("the test sheet is not clean: %v", file.Problems)
	}
	return file
}

func importRequired(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, in store.InvoiceImport) store.InvoiceImportResult {
	t.Helper()
	res, err := importInvoices(t, st, tenantID, actorID, in)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res
}

func reconcile(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, p store.ReconcileParams) (store.ReconcileResult, error) {
	t.Helper()
	var res store.ReconcileResult
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		res, err = q.Reconcile(context.Background(), tenantID, actorID, p)
		return err
	})
	return res, err
}

// TestImportStockBringsTheYardIn: the sheet is what the app knows about units
// that have not been sold yet, and every unit arrives with its RECEIVE row.
func TestImportStockBringsTheYardIn(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := stockFile(t,
		map[string]string{},
		map[string]string{
			"stock_no": "S-02", "chassis_no": "ZAPM0000001002", "engine_no": "ENG-1002",
			"received_on": "", "status": "RESERVED", "notes": "Held for a customer",
		},
	)
	in := store.StockImport{
		Filename: "stock.csv", SHA256: testFileSHA, DefaultMake: "Piaggio",
		ReceivedOn: parseDate(t, "2026-10-30"), File: file,
	}

	res, err := importStock(t, st, alpha.ID, owner.ID, in)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 2 || res.Skipped != 0 {
		t.Fatalf("imported %d skipped %d, want 2 and 0", res.Imported, res.Skipped)
	}
	if res.Totals != file.Totals {
		t.Fatalf("totals = %+v, want the sheet's %+v", res.Totals, file.Totals)
	}

	if got := vehicleStatus(t, st, alpha.ID, "ZAPM0000001001"); got != store.StatusAvailable {
		t.Errorf("the first unit is %s, want available", got)
	}
	if got := vehicleStatus(t, st, alpha.ID, "ZAPM0000001002"); got != store.StatusReserved {
		t.Errorf("the reserved unit is %s", got)
	}
	if summary := stockSummary(t, st, alpha.ID); summary.LedgerTotal != 2 || summary.Available != 1 || summary.Reserved != 1 {
		t.Errorf("summary = %+v", summary)
	}

	// The same sheet again changes nothing, not even the ledger.
	second, err := importStock(t, st, alpha.ID, owner.ID, in)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Imported != 0 || second.Skipped != 2 {
		t.Fatalf("second import wrote %d and skipped %d", second.Imported, second.Skipped)
	}
	if summary := stockSummary(t, st, alpha.ID); summary.LedgerTotal != 2 {
		t.Errorf("ledger total after importing twice = %d, want 2", summary.LedgerTotal)
	}
}

// TestImportStockRefusesAYardItCannotBelieve: a unit already sold cannot be
// standing in the yard, and a different price for a unit already in stock is a
// disagreement, not an update.
func TestImportStockRefusesAYardItCannotBelieve(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	in := store.StockImport{
		Filename: "stock.csv", SHA256: testFileSHA, DefaultMake: "Piaggio",
		ReceivedOn: parseDate(t, "2026-10-30"), File: stockFile(t, map[string]string{}),
	}
	if _, err := importStock(t, st, alpha.ID, owner.ID, in); err != nil {
		t.Fatalf("import: %v", err)
	}

	in.File = stockFile(t, map[string]string{"price": "1300000.00"})
	_, err := importStock(t, st, alpha.ID, owner.ID, in)
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("a changed price = %v, want a refusal", err)
	}

	// The unit leaves the yard with a sale, and the sheet listing it again has
	// to be refused: the sheet is not the book.
	if _, err := importInvoices(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio",
		File: invoiceFile(t, map[string]string{"invoice_no": "0070", "chassis": "ZAPM0000001001"}),
	}); err != nil {
		t.Fatalf("sale of the imported unit: %v", err)
	}
	in.File = stockFile(t, map[string]string{"received_on": "2026-10-01"})
	_, err = importStock(t, st, alpha.ID, owner.ID, in)
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("a sold unit on the sheet = %v, want a refusal", err)
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Problems[0].Problem, "sold") {
		t.Fatalf("problems = %+v", refusal)
	}
}

// TestImportStockNeedsNoDefaultMakeWhenTheSheetNamesOne: the sheet may name a
// make row by row, and a default that is never used should not be demanded.
func TestImportStockNeedsNoDefaultMakeWhenTheSheetNamesOne(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	res, err := importStock(t, st, alpha.ID, owner.ID, store.StockImport{
		Filename: "stock.csv", SHA256: testFileSHA,
		ReceivedOn: parseDate(t, "2026-10-30"), File: stockFile(t, map[string]string{}),
	})
	if err != nil {
		t.Fatalf("import without a default make: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("imported %d units, want 1", res.Imported)
	}

	// A row that leaves the make blank still owes an answer.
	_, err = importStock(t, st, alpha.ID, owner.ID, store.StockImport{
		Filename: "stock.csv", SHA256: testFileSHA,
		ReceivedOn: parseDate(t, "2026-10-30"),
		File:       stockFile(t, map[string]string{"make": "", "chassis_no": "ZAPM0000001009", "engine_no": "ENG-1009"}),
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("a sheet with no make anywhere = %v, want a refusal", err)
	}
}

// TestReconcileAgreesWithTheFileAfterAnImport is the daily check of the
// parallel month: upload the file, import it, and prove the book says the same.
func TestReconcileAgreesWithTheFileAfterAnImport(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	file := invoiceFile(t,
		map[string]string{"invoice_no": "0080"},
		map[string]string{
			"invoice_no": "0081", "chassis": "ZAPM0000000081", "engine": "ENG-81",
			"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
		},
	)
	importRequired(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: file,
	})

	res, err := reconcile(t, st, alpha.ID, owner.ID, store.ReconcileParams{
		Filename: "invoices.csv", SHA256: testFileSHA, File: file,
		DateFrom: parseDate(t, "2026-10-01"), DateTo: parseDate(t, "2026-10-31"),
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !res.Run.Matched {
		t.Fatalf("a freshly imported file does not match: %+v", res.Run.Difference)
	}
	if len(res.Lines) != 2 {
		t.Fatalf("lines = %d, want one for cash and one for leasing", len(res.Lines))
	}
	for _, line := range res.Lines {
		if line.Source != line.Stored {
			t.Errorf("%s %s: the file says %+v and the book says %+v", line.Day.Format("2006-01-02"),
				line.DocType, line.Source, line.Stored)
		}
		if !line.Source.Any() {
			t.Errorf("%s %s counted nothing", line.Day.Format("2006-01-02"), line.DocType)
		}
	}

	if alerts, err := listAlerts(t, st, alpha.ID); err != nil || len(alerts) != 0 {
		t.Fatalf("alerts = %+v (%v), want none", alerts, err)
	}
}

// TestReconcileAlertsWhenTheBookDrifts: a paper the file has and the book does
// not is exactly the difference the parallel month is looking for.
func TestReconcileAlertsWhenTheBookDrifts(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	imported := invoiceFile(t, map[string]string{"invoice_no": "0090"})
	importRequired(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio", File: imported,
	})

	// The next day's file has one more sale in it, which has not been imported.
	tomorrow := invoiceFile(t,
		map[string]string{"invoice_no": "0090"},
		map[string]string{"invoice_no": "0091", "date": "2026-10-31", "chassis": "ZAPM0000000091", "engine": "ENG-91"},
	)
	res, err := reconcile(t, st, alpha.ID, owner.ID, store.ReconcileParams{
		Filename: "invoices.csv", SHA256: testFileSHA, File: tomorrow,
		DateFrom: parseDate(t, "2026-10-01"), DateTo: parseDate(t, "2026-10-31"),
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Run.Matched {
		t.Fatal("the run matches, but the file has a paper the book does not")
	}
	if res.Run.Difference.Papers != -1 {
		t.Errorf("difference = %d papers, want -1 (the book is one short)", res.Run.Difference.Papers)
	}
	if res.Run.Difference.GrossCents != -125000000 {
		t.Errorf("money difference = %d, want -125000000", res.Run.Difference.GrossCents)
	}

	alerts, err := listAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("alerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Kind != store.AlertReconciliation {
		t.Fatalf("alerts = %+v, want one reconciliation alert", alerts)
	}
	if !strings.Contains(alerts[0].Message, "1 fewer paper") {
		t.Errorf("alert message = %q", alerts[0].Message)
	}
	if !strings.Contains(alerts[0].Message, "2026-10-31") {
		t.Errorf("the alert does not point at the day that differs: %q", alerts[0].Message)
	}
	if alerts[0].Severity != store.SeverityWarning {
		t.Errorf("severity = %q, want warning", alerts[0].Severity)
	}
}

// TestReconcileRefusesARangeWithNothingInIt: a green run over no paper proves
// nothing, so it is not recorded as one.
func TestReconcileRefusesARangeWithNothingInIt(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	_, err := reconcile(t, st, alpha.ID, owner.ID, store.ReconcileParams{
		Filename: "invoices.csv", SHA256: testFileSHA, File: invoiceFile(t),
		DateFrom: parseDate(t, "2026-10-01"), DateTo: parseDate(t, "2026-10-31"),
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("reconcile over an empty range = %v, want a refusal", err)
	}

	_, err = reconcile(t, st, alpha.ID, owner.ID, store.ReconcileParams{
		Filename: "invoices.csv", SHA256: testFileSHA, File: invoiceFile(t),
		DateFrom: parseDate(t, "2026-10-31"), DateTo: parseDate(t, "2026-10-01"),
	})
	if !errors.Is(err, store.ErrImportRefused) {
		t.Fatalf("a range the wrong way round = %v, want a refusal", err)
	}

	var runs []store.ReconcileRun
	if err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		var err error
		runs, err = q.ListReconciliationRuns(context.Background(), alpha.ID, 10)
		return err
	}); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("a refused comparison was recorded: %+v", runs)
	}
}

// TestNumberingGapsUnderstandImportedNumbers: the old app spent one SALES
// number space for cash and leasing, so a number held by the other series is a
// number somebody really has, not a gap. A number nobody holds is still a gap.
func TestNumberingGapsUnderstandImportedNumbers(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	// Cash 0001, leasing 0002: one space, no gap in either series.
	importRequired(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "invoices.csv", SHA256: testFileSHA, DefaultMake: "Piaggio",
		File: invoiceFile(t,
			map[string]string{"invoice_no": "0001"},
			map[string]string{
				"invoice_no": "0002", "chassis": "ZAPM0000000002", "engine": "ENG-2",
				"price": "1250000.0", "down": "250000.0", "balance": "1000000.0",
			},
		),
	})

	gaps := scanGaps(t, st, alpha.ID, 2026)
	if len(gaps) != 0 {
		t.Fatalf("gaps after importing one shared series = %+v, want none", gaps)
	}

	// A file that jumps from 0003 to 0006 really did lose numbers.
	importRequired(t, st, alpha.ID, owner.ID, store.InvoiceImport{
		Filename: "later.csv", SHA256: testFileSHA, DefaultMake: "Piaggio",
		File: invoiceFile(t,
			map[string]string{"invoice_no": "0003", "chassis": "ZAPM0000000003", "engine": "ENG-3"},
			map[string]string{"invoice_no": "0006", "chassis": "ZAPM0000000006", "engine": "ENG-6"},
		),
	})

	gaps = scanGaps(t, st, alpha.ID, 2026)
	if len(gaps) != 2 {
		t.Fatalf("gaps = %+v, want one row for each sales series", gaps)
	}
	for _, gap := range gaps {
		if gap.FirstMissing != 4 || gap.Missing != 2 || gap.Expected != 6 || gap.Issued != 4 {
			t.Errorf("%s: expected %d issued %d first missing %d missing %d, want 6/4/4/2",
				gap.Series, gap.Expected, gap.Issued, gap.FirstMissing, gap.Missing)
		}
	}
}

func scanGaps(t *testing.T, st *store.Store, tenantID uuid.UUID, year int) []store.NumberingGap {
	t.Helper()
	var gaps []store.NumberingGap
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		gaps, err = q.ScanNumberingGaps(context.Background(), tenantID, year)
		return err
	})
	if err != nil {
		t.Fatalf("scan gaps: %v", err)
	}
	return gaps
}
