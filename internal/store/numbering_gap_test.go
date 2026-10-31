package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// TestCloseDayAlertsOnAVariancePastTheLimit proves C7: a drawer past the
// tenant's threshold is told to somebody, in the same transaction as the
// close. Under the threshold nothing fires, because a till that is a few
// cents out at the end of a day is ordinary counter life.
func TestCloseDayAlertsOnAVariancePastTheLimit(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	window := todayWindow(t)

	// One leasing sale today with 40,000.00 down left to settle, and a cash
	// receipt of 100,000.00 against it. That is the drawer's expected cash.
	doc := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000003001", 100000000, 40000000, window.Date)
	cash := cashPayment(doc.ID, 10000000)
	cash.PaidOn = window.Date
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cash); err != nil {
		t.Fatalf("cash payment: %v", err)
	}

	// The owner will tolerate a difference of up to 1,000.00.
	saveSettings(t, st, alpha.ID, owner.ID, store.SettingsParams{
		CompanyName:        "Alpha Motors",
		VarianceAlertCents: 100000,
	})

	// A drawer 500.00 short is inside the tolerance, so no alert.
	end, err := closeDay(t, st, alpha.ID, owner.ID, window, 0, 9950000)
	if err != nil {
		t.Fatalf("close inside tolerance: %v", err)
	}
	if end.VarianceCents != -50000 {
		t.Fatalf("variance = %d, want -50000", end.VarianceCents)
	}
	alerts, err := listAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("alerts: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("alerts inside tolerance = %+v, want none", alerts)
	}

	// A drawer 5,000.00 short is past it, and short is the worse direction,
	// so it is critical. The next day is a fresh close, because a day is
	// closed once.
	window2 := store.DayWindow{Date: window.Date.AddDate(0, 0, 1)}
	window2.Start = window2.Date
	window2.End = window2.Date.AddDate(0, 0, 1)
	doc2 := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000003002", 100000000, 40000000, window2.Date)
	cash2 := cashPayment(doc2.ID, 10000000)
	cash2.PaidOn = window2.Date
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cash2); err != nil {
		t.Fatalf("cash payment day two: %v", err)
	}
	end2, err := closeDay(t, st, alpha.ID, owner.ID, window2, 0, 5000000)
	if err != nil {
		t.Fatalf("close past tolerance: %v", err)
	}
	if end2.VarianceCents != -5000000 {
		t.Fatalf("variance = %d, want -5000000", end2.VarianceCents)
	}
	alerts, err = listAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want exactly one", alerts)
	}
	a := alerts[0]
	if a.Kind != store.AlertVariance {
		t.Errorf("kind = %q, want VARIANCE", a.Kind)
	}
	if a.Severity != store.SeverityCritical {
		t.Errorf("severity = %q, want critical for a short drawer", a.Severity)
	}
	// The message must carry the numbers a person needs to go and check.
	for _, want := range []string{"short", end2.ZNo, "50,000.00"} {
		if !strings.Contains(a.Message, want) {
			t.Errorf("message %q missing %q", a.Message, want)
		}
	}
}

// TestCloseDayToleratesNothingWhenNoLimitIsSet proves the default is the
// careful one: a tenant that has never opened the settings screen alerts on
// any difference at all, rather than silently tolerating an unbounded one.
func TestCloseDayToleratesNothingWhenNoLimitIsSet(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	window := todayWindow(t)

	// A cash receipt of 100,000.00 is the drawer's expected cash.
	doc := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000006001", 100000000, 40000000, window.Date)
	cash := cashPayment(doc.ID, 10000000)
	cash.PaidOn = window.Date
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cash); err != nil {
		t.Fatalf("cash payment: %v", err)
	}

	// One cent short, no settings saved at all: the careful default alerts on
	// any difference rather than tolerating an unbounded one.
	if _, err := closeDay(t, st, alpha.ID, owner.ID, window, 0, 9999999); err != nil {
		t.Fatalf("close: %v", err)
	}
	alerts, err := listAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("alerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Kind != store.AlertVariance {
		t.Fatalf("alerts = %+v, want one variance alert on a one cent difference", alerts)
	}
}

// TestNumberingGapIsFoundAndAlerted proves C2: a number that was spent with no
// paper carrying it is found by the scan and alerted, not tolerated. The gap
// is manufactured the only way it can be, by moving the counter forward
// without writing a document.
func TestNumberingGapIsFoundAndAlerted(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v1 := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000004001", 125000000)
	v2 := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000004002", 125000000)
	v3 := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000004003", 125000000)

	year := today.Year()
	for i, v := range []store.Vehicle{v1, v2, v3} {
		if i == 1 {
			// Spend a number with no paper behind it: 0002 goes missing.
			if err := bumpSeries(t, alpha.ID, store.DocSalesCash, year); err != nil {
				t.Fatalf("bump series: %v", err)
			}
		}
		res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
			DocType:    store.DocSalesCash,
			DocDate:    today,
			CustomerID: customer.ID,
			VehicleID:  v.ID,
			PriceCents: 125000000,
			Today:      today,
		})
		if err != nil {
			t.Fatalf("sale %d: %v", i, err)
		}
		t.Logf("issued %s", res.Document.DocNo)
	}

	// The scan finds the series is behind its counter.
	var gaps []store.NumberingGap
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var innerErr error
		gaps, innerErr = q.ScanNumberingGaps(ctx, alpha.ID, year)
		return innerErr
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %+v, want exactly one", gaps)
	}
	g := gaps[0]
	if g.Series != store.DocSalesCash || g.Year != year {
		t.Errorf("gap = %+v, want the cash invoice series for this year", g)
	}
	if g.FirstMissing != 2 {
		t.Errorf("first missing = %d, want 2", g.FirstMissing)
	}
	if g.Missing != 1 || g.Expected != 4 || g.Issued != 3 {
		t.Errorf("gap = %+v, want expected 4, issued 3, missing 1", g)
	}

	// And the day end turns it into an alert with the number in it.
	count, err := recordGapAlerts(t, st, alpha.ID, year)
	if err != nil {
		t.Fatalf("record gap alerts: %v", err)
	}
	if count != 1 {
		t.Fatalf("recorded %d alerts, want 1", count)
	}
	alerts, err := listAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("alerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Kind != store.AlertNumberingGap {
		t.Fatalf("alerts = %+v, want one numbering gap alert", alerts)
	}
	if !strings.Contains(alerts[0].Message, "0002") {
		t.Errorf("message %q missing the missing number 0002", alerts[0].Message)
	}
	if !strings.Contains(alerts[0].Message, "cash invoices") {
		t.Errorf("message %q does not name the series in words", alerts[0].Message)
	}
	if alerts[0].Severity != store.SeverityCritical {
		t.Errorf("severity = %q, want critical", alerts[0].Severity)
	}
}

// TestNoGapOnAWholeSeries proves the scan is quiet when nothing is missing,
// which is the state every dealership is in almost every day.
func TestNoGapOnAWholeSeries(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000005001", 125000000)
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
		Today:      today,
	}); err != nil {
		t.Fatalf("sale: %v", err)
	}

	var gaps []store.NumberingGap
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var innerErr error
		gaps, innerErr = q.ScanNumberingGaps(ctx, alpha.ID, today.Year())
		return innerErr
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("gaps = %+v, want none on a whole series", gaps)
	}
}

// bumpSeries moves a series' counter forward by one without writing a
// document, which is the only way a gap can appear in a system that allocates
// numbers inside the paper's own transaction. It writes the counter the way a
// rolled back allocation would have left it, on one connection that carries
// the tenant config so row level security sees the right dealership.
func bumpSeries(t *testing.T, tenantID uuid.UUID, series string, year int) error {
	t.Helper()
	if runtime == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := runtime.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `select set_config('app.tenant_id', $1, false)`, tenantID.String()); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `
		insert into document_series (tenant_id, doc_type, year, last_value)
		values ($1, $2, $3, 1)
		on conflict (tenant_id, doc_type, year) do update
		set last_value = document_series.last_value + 1`, tenantID, series, year)
	return err
}

func recordGapAlerts(t *testing.T, st *store.Store, tenantID uuid.UUID, year int) (int, error) {
	t.Helper()
	var n int
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		n, err = q.RecordNumberingGapAlerts(context.Background(), tenantID, year)
		return err
	})
	return n, err
}
