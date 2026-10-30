package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// vehicleReceivedOn creates a unit that has been standing in the yard since the
// given date, which is what the ageing bucket is measured from.
func vehicleReceivedOn(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, chassis string, price, cost int64, received time.Time) store.Vehicle {
	t.Helper()
	var v store.Vehicle
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		v, err = q.CreateVehicle(context.Background(), tenantID, actorID, store.VehicleParams{
			Make:       "Piaggio",
			Model:      "APE City",
			Type:       "Petrol",
			Variant:    "City",
			ChassisNo:  chassis,
			EngineNo:   "ENG-" + chassis[len(chassis)-4:],
			Colour:     "White",
			YOM:        int32Ptr(2026),
			CostCents:  cost,
			PriceCents: price,
			ReceivedAt: received,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create vehicle: %v", err)
	}
	return v
}

func TestSalesReportCountsInvoicesInThePeriod(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	clerk := newUser(t, st, alpha.ID, "sales@alpha.test", "SALES")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}

	// Cash sold by the owner, leasing sold by the clerk, a quotation that is
	// not a sale, and an invoice from before the period.
	cashUnit := vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004001", 125000000, 110000000, parseDate(t, "2026-10-01"))
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-05"),
		CustomerID: customer.ID,
		VehicleID:  cashUnit.ID,
		PriceCents: 125000000,
	}); err != nil {
		t.Fatalf("cash sale: %v", err)
	}

	leaseUnit := vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004002", 100000000, 90000000, parseDate(t, "2026-10-01"))
	if _, err := createDocument(t, st, alpha.ID, clerk.ID, store.DocumentParams{
		DocType:        store.DocSalesLease,
		DocDate:        parseDate(t, "2026-10-06"),
		CustomerID:     customer.ID,
		VehicleID:      leaseUnit.ID,
		PriceCents:     100000000,
		DownCents:      40000000,
		FinanceCompany: "SINGER FINANCE",
	}); err != nil {
		t.Fatalf("leasing sale: %v", err)
	}

	quoteUnit := vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004003", 90000000, 80000000, parseDate(t, "2026-10-01"))
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocQuotation,
		DocDate:    parseDate(t, "2026-10-07"),
		CustomerID: customer.ID,
		VehicleID:  quoteUnit.ID,
		PriceCents: 90000000,
	}); err != nil {
		t.Fatalf("quotation: %v", err)
	}

	oldUnit := vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004004", 75000000, 70000000, parseDate(t, "2026-09-01"))
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-09-30"),
		CustomerID: customer.ID,
		VehicleID:  oldUnit.ID,
		PriceCents: 75000000,
	}); err != nil {
		t.Fatalf("september sale: %v", err)
	}

	var report store.SalesReport
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		report, err = q.SalesReportFor(ctx, alpha.ID, parseDate(t, "2026-10-01"), parseDate(t, "2026-10-31"), nil)
		return err
	})
	if err != nil {
		t.Fatalf("sales report: %v", err)
	}

	if len(report.ByType) != 2 {
		t.Fatalf("by type = %+v", report.ByType)
	}
	cash, lease := report.ByType[0], report.ByType[1]
	if cash.DocType != store.DocSalesCash || cash.Count != 1 || cash.GrossCents != 125000000 ||
		cash.DownCents != 125000000 || cash.BalanceCents != 0 {
		t.Fatalf("cash row = %+v", cash)
	}
	if lease.DocType != store.DocSalesLease || lease.Count != 1 || lease.GrossCents != 100000000 ||
		lease.DownCents != 40000000 || lease.BalanceCents != 60000000 {
		t.Fatalf("lease row = %+v", lease)
	}

	want := store.SalesTotal{Count: 2, GrossCents: 225000000, DownCents: 165000000, BalanceCents: 60000000}
	if report.Total != want {
		t.Fatalf("total = %+v, want %+v", report.Total, want)
	}

	// Largest seller first: the owner sold the 1,250,000.00 cash invoice, the
	// clerk the 1,000,000.00 leasing one.
	if len(report.ByUser) != 2 {
		t.Fatalf("by user = %+v", report.ByUser)
	}
	if report.ByUser[0].Count != 1 || report.ByUser[0].GrossCents != 125000000 {
		t.Fatalf("first seller = %+v", report.ByUser[0])
	}
	if report.ByUser[1].Count != 1 || report.ByUser[1].GrossCents != 100000000 {
		t.Fatalf("second seller = %+v", report.ByUser[1])
	}
}

func TestReceivablesAgeAndGroupByFinance(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}

	asOf := parseDate(t, "2026-10-30")

	// One invoice per bucket, each 600,000.00 owed.
	leasing := func(chassis, on, company string) store.Document {
		t.Helper()
		v := vehicleReceivedOn(t, st, alpha.ID, owner.ID, chassis, 100000000, 90000000, parseDate(t, "2026-07-01"))
		res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
			DocType:        store.DocSalesLease,
			DocDate:        parseDate(t, on),
			CustomerID:     customer.ID,
			VehicleID:      v.ID,
			PriceCents:     100000000,
			DownCents:      40000000,
			FinanceCompany: company,
		})
		if err != nil {
			t.Fatalf("leasing sale %s: %v", chassis, err)
		}
		return res.Document
	}

	// 5 days old.
	fresh := leasing("ZAPM0000004101", "2026-10-25", "SINGER FINANCE")
	// 60 days old: the last day of the 31-60 bucket.
	leasing("ZAPM0000004102", "2026-08-31", "SINGER FINANCE")
	// 120 days old.
	old := leasing("ZAPM0000004103", "2026-06-30", "LOLC FINANCE")

	// Half of the fresh invoice is paid, so it is owed less than the others.
	if _, err := postPayment(t, st, alpha.ID, owner.ID, store.PaymentParams{
		DocumentID:  fresh.ID,
		PaidOn:      parseDate(t, "2026-10-28"),
		Method:      store.MethodBank,
		AmountCents: 30000000,
	}); err != nil {
		t.Fatalf("part payment: %v", err)
	}

	// An invoice paid in full is no longer receivable at all.
	settled := leasing("ZAPM0000004104", "2026-10-20", "SINGER FINANCE")
	if _, err := postPayment(t, st, alpha.ID, owner.ID, store.PaymentParams{
		DocumentID:  settled.ID,
		PaidOn:      parseDate(t, "2026-10-21"),
		Method:      store.MethodCash,
		AmountCents: 60000000,
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	var report store.ReceivablesReport
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		report, err = q.ReceivablesReportFor(ctx, alpha.ID, asOf, nil)
		return err
	})
	if err != nil {
		t.Fatalf("receivables: %v", err)
	}

	if len(report.Rows) != 3 {
		t.Fatalf("rows = %d, want 3 (%+v)", len(report.Rows), report.Rows)
	}
	// Oldest first, so the report reads as a reminder list.
	if report.Rows[0].DocNo != old.DocNo || report.Rows[2].DocNo != fresh.DocNo {
		t.Fatalf("order = %s, %s, %s", report.Rows[0].DocNo, report.Rows[1].DocNo, report.Rows[2].DocNo)
	}
	if report.Rows[2].OutstandingCents != 30000000 || report.Rows[2].PaidCents != 30000000 {
		t.Fatalf("fresh invoice = %+v", report.Rows[2])
	}

	buckets := make(map[string]store.ReceivableBucketRow)
	for _, b := range report.Buckets {
		buckets[b.Bucket] = b
	}
	for _, want := range []struct {
		bucket  string
		count   int
		amount  int64
		missing bool
	}{
		{"0-30", 1, 30000000, false},
		{"31-60", 1, 60000000, false},
		{"61-90", 0, 0, true},
		{"90+", 1, 60000000, false},
	} {
		got, ok := buckets[want.bucket]
		if want.missing {
			if ok {
				t.Fatalf("bucket %s should be empty: %+v", want.bucket, got)
			}
			continue
		}
		if !ok {
			t.Fatalf("no %s bucket in %+v", want.bucket, report.Buckets)
		}
		if got.Count != want.count || got.OutstandingCents != want.amount {
			t.Fatalf("bucket %s = %+v", want.bucket, got)
		}
	}

	if report.TotalOutstandingCents != 150000000 {
		t.Fatalf("total outstanding = %d, want 150000000", report.TotalOutstandingCents)
	}
	if len(report.ByFinance) != 2 {
		t.Fatalf("by finance = %+v", report.ByFinance)
	}
	// Largest first: Singer owes 90,000.00, LOLC 60,000.00.
	if report.ByFinance[0].FinanceCompany != "SINGER FINANCE" || report.ByFinance[0].Count != 2 ||
		report.ByFinance[0].OutstandingCents != 90000000 {
		t.Fatalf("singer row = %+v", report.ByFinance[0])
	}
	if report.ByFinance[1].FinanceCompany != "LOLC FINANCE" || report.ByFinance[1].OutstandingCents != 60000000 {
		t.Fatalf("lolc row = %+v", report.ByFinance[1])
	}
}

// A receivables report is read as at a business date, so it says what was owed
// on that date, not what is owed now. Money taken later does not reach back and
// reduce it; money handed back later still counted as paid on that date.
func TestReceivablesReadAsAtTheBusinessDate(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	// Invoiced ten days before the report date, 600,000.00 owed.
	sold := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000004301", 100000000, 40000000, parseDate(t, "2026-10-20"))

	// Taken before the report date and handed back after it. On 30 October the
	// customer had still given us this money, so the day's receivables are
	// short by it.
	receipt, err := postPayment(t, st, alpha.ID, owner.ID, store.PaymentParams{
		DocumentID:  sold.ID,
		PaidOn:      parseDate(t, "2026-10-26"),
		Method:      store.MethodCash,
		AmountCents: 20000000,
	})
	if err != nil {
		t.Fatalf("payment before the report date: %v", err)
	}
	if _, err := reversePayment(t, st, alpha.ID, owner.ID, receipt.ID, "the customer cancelled", parseDate(t, "2026-11-02")); err != nil {
		t.Fatalf("reversal after the report date: %v", err)
	}

	// Taken after the report date, so it does not reduce what was owed on 30
	// October, however true it is today.
	if _, err := postPayment(t, st, alpha.ID, owner.ID, store.PaymentParams{
		DocumentID:  sold.ID,
		PaidOn:      parseDate(t, "2026-11-05"),
		Method:      store.MethodBank,
		AmountCents: 10000000,
	}); err != nil {
		t.Fatalf("payment after the report date: %v", err)
	}

	// An invoice raised after the report date is not in it either.
	leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000004302", 80000000, 30000000, parseDate(t, "2026-11-03"))

	read := func(asOf time.Time) store.ReceivablesReport {
		t.Helper()
		var report store.ReceivablesReport
		err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
			var err error
			report, err = q.ReceivablesReportFor(ctx, alpha.ID, asOf, nil)
			return err
		})
		if err != nil {
			t.Fatalf("receivables as at %s: %v", asOf.Format("2006-01-02"), err)
		}
		return report
	}

	at := read(parseDate(t, "2026-10-30"))
	if len(at.Rows) != 1 || at.Rows[0].DocNo != sold.DocNo {
		t.Fatalf("rows as at 30 October = %+v, want only %s", at.Rows, sold.DocNo)
	}
	if at.Rows[0].PaidCents != 20000000 || at.Rows[0].OutstandingCents != 40000000 {
		t.Fatalf("as at 30 October = %+v, want 20000000 paid and 40000000 outstanding", at.Rows[0])
	}
	if at.TotalOutstandingCents != 40000000 {
		t.Fatalf("total as at 30 October = %d, want 40000000", at.TotalOutstandingCents)
	}

	// Today the reversal has landed and the later receipt is real, so the same
	// invoice is owed more again, and the newer invoice is owed too.
	now := read(parseDate(t, "2026-11-10"))
	if len(now.Rows) != 2 {
		t.Fatalf("rows as at 10 November = %+v, want both invoices", now.Rows)
	}
	if now.Rows[0].DocNo != sold.DocNo || now.Rows[0].PaidCents != 10000000 || now.Rows[0].OutstandingCents != 50000000 {
		t.Fatalf("as at 10 November = %+v, want 10000000 paid and 50000000 outstanding", now.Rows[0])
	}
	if now.TotalOutstandingCents != 100000000 {
		t.Fatalf("total as at 10 November = %d, want 100000000", now.TotalOutstandingCents)
	}
}

func TestStockReportValuesAndAgesTheYard(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}

	asOf := parseDate(t, "2026-10-30")

	// 1,000,000.00 price, 900,000.00 cost on every unit.
	vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004201", 100000000, 90000000, parseDate(t, "2026-10-30"))
	reserved := vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004202", 100000000, 90000000, parseDate(t, "2026-08-01"))
	sold := vehicleReceivedOn(t, st, alpha.ID, owner.ID, "ZAPM0000004203", 100000000, 90000000, parseDate(t, "2026-10-29"))

	// The deposit reserves the second unit.
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocAdvance,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  reserved.ID,
		PriceCents: 100000000,
		DownCents:  10000000,
	}); err != nil {
		t.Fatalf("advance receipt: %v", err)
	}
	// The sale takes the third unit out of the yard.
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  sold.ID,
		PriceCents: 100000000,
	}); err != nil {
		t.Fatalf("cash sale: %v", err)
	}

	var report store.StockReport
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		report, err = q.StockReportFor(ctx, alpha.ID, asOf, true)
		return err
	})
	if err != nil {
		t.Fatalf("stock report: %v", err)
	}

	if len(report.Statuses) != 3 {
		t.Fatalf("statuses = %+v", report.Statuses)
	}
	if report.Statuses[0].Status != store.StatusAvailable || report.Statuses[0].Count != 1 {
		t.Fatalf("first status = %+v", report.Statuses[0])
	}
	if report.Statuses[1].Status != store.StatusReserved || report.Statuses[1].Count != 1 {
		t.Fatalf("second status = %+v", report.Statuses[1])
	}
	if report.Statuses[2].Status != store.StatusSold || report.Statuses[2].Count != 1 {
		t.Fatalf("third status = %+v", report.Statuses[2])
	}
	if report.Statuses[0].CostCents != 90000000 || report.Statuses[0].PriceCents != 100000000 {
		t.Fatalf("available value = %+v", report.Statuses[0])
	}

	// The sold unit has left, so only two are on hand.
	if report.OnHand != 2 || report.OnHandCost != 180000000 || report.OnHandSale != 200000000 {
		t.Fatalf("on hand = %d cost %d sale %d", report.OnHand, report.OnHandCost, report.OnHandSale)
	}

	ages := make(map[string]store.StockAgeRow)
	for _, row := range report.Ageing {
		ages[row.Bucket] = row
	}
	if len(report.Ageing) != 2 {
		t.Fatalf("ageing = %+v", report.Ageing)
	}
	if ages["0-30"].Count != 1 || ages["61-90"].Count != 1 {
		t.Fatalf("ageing = %+v", report.Ageing)
	}

	// Without the cost permission the report still counts, but carries no cost.
	var hidden store.StockReport
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		hidden, err = q.StockReportFor(ctx, alpha.ID, asOf, false)
		return err
	})
	if err != nil {
		t.Fatalf("stock report without cost: %v", err)
	}
	if hidden.OnHand != report.OnHand || hidden.OnHandCost != 0 || hidden.OnHandSale != report.OnHandSale {
		t.Fatalf("hidden cost report = %+v", hidden)
	}
	for _, row := range hidden.Statuses {
		if row.CostCents != 0 {
			t.Fatalf("cost leaked without permission: %+v", row)
		}
	}
}

func TestAgeBucketBoundaries(t *testing.T) {
	for days, want := range map[int]string{
		-5: "0-30", 0: "0-30", 30: "0-30",
		31: "31-60", 60: "31-60",
		61: "61-90", 90: "61-90",
		91: "90+", 4000: "90+",
	} {
		if got := store.AgeBucket(days); got != want {
			t.Fatalf("AgeBucket(%d) = %s, want %s", days, got, want)
		}
	}
}

func TestReportsAreInvisibleToOtherTenants(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	ownerAlpha := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	newUser(t, st, beta.ID, "owner@beta.test", "OWNER")
	asOf := parseDate(t, "2026-10-30")

	leasedSale(t, st, alpha.ID, ownerAlpha.ID, "ZAPM0000004301", 100000000, 40000000)

	err := st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		sales, err := q.SalesReportFor(ctx, beta.ID, parseDate(t, "2026-01-01"), parseDate(t, "2026-12-31"), nil)
		if err != nil {
			return err
		}
		if sales.Total.Count != 0 || len(sales.ByType) != 0 || len(sales.ByUser) != 0 {
			t.Fatalf("beta sees alpha's sales: %+v", sales)
		}

		receivables, err := q.ReceivablesReportFor(ctx, beta.ID, asOf, nil)
		if err != nil {
			return err
		}
		if len(receivables.Rows) != 0 || receivables.TotalOutstandingCents != 0 {
			t.Fatalf("beta sees alpha's receivables: %+v", receivables)
		}
		rows, err := q.ReceivablesFor(ctx, beta.ID, asOf, nil, 100)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatalf("beta sees %d receivables", len(rows))
		}

		stock, err := q.StockReportFor(ctx, beta.ID, asOf, true)
		if err != nil {
			return err
		}
		if len(stock.Statuses) != 0 || stock.OnHand != 0 {
			t.Fatalf("beta sees alpha's yard: %+v", stock)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("beta: %v", err)
	}

	// Alpha still has its own numbers.
	var alphaSales store.SalesReport
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		alphaSales, err = q.SalesReportFor(ctx, alpha.ID, parseDate(t, "2026-01-01"), parseDate(t, "2026-12-31"), nil)
		return err
	})
	if err != nil {
		t.Fatalf("alpha sales: %v", err)
	}
	if alphaSales.Total.Count != 1 {
		t.Fatalf("alpha sales = %+v, want 1 invoice", alphaSales.Total)
	}
}
