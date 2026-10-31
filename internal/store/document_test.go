package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

func sellableVehicle(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, chassis string, price int64) store.Vehicle {
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
			CostCents:  price - 1000000,
			PriceCents: price,
			ReceivedAt: parseDate(t, "2026-10-30"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("create vehicle: %v", err)
	}
	return v
}

func int32Ptr(v int32) *int32 { return &v }

func createDocument(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, p store.DocumentParams) (store.DocumentResult, error) {
	t.Helper()
	// Tests that predate approvals date their documents and never declare a
	// business day. For those, the document is issued the day it is dated, so
	// the backdate rule never trips. A test that wants to prove the rule
	// passes Today itself and this default stays out of the way.
	if p.Today.IsZero() && !p.DocDate.IsZero() {
		p.Today = p.DocDate
	}
	var res store.DocumentResult
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		res, err = q.CreateDocument(context.Background(), tenantID, actorID, p)
		return err
	})
	return res, err
}

func TestCashSaleSellsTheUnit(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000100", 125000000)

	res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
		DownCents:  1, // ignored: cash is paid in full
	})
	if err != nil {
		t.Fatalf("cash sale: %v", err)
	}

	d := res.Document
	if d.DocNo != "0001" || d.Status != store.DocIssued {
		t.Fatalf("document = %s %s", d.DocNo, d.Status)
	}
	if d.PriceCents != 125000000 || d.DownCents != 125000000 || d.BalanceCents != 0 {
		t.Fatalf("cash money = price %d down %d balance %d", d.PriceCents, d.DownCents, d.BalanceCents)
	}
	if d.CustomerName != "Saman Perera" || d.VehicleChassisNo != v.ChassisNo || d.VehicleType != "Petrol" {
		t.Fatalf("snapshot = %+v", d)
	}
	if !res.StockMoved || res.VehicleAfter != store.StatusSold {
		t.Fatalf("stock result = %+v", res)
	}

	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		after, err := q.VehicleByID(ctx, alpha.ID, v.ID)
		if err != nil {
			return err
		}
		if after.Status != store.StatusSold {
			t.Fatalf("vehicle status = %s, want SOLD", after.Status)
		}
		summary, err := q.StockSummaryFor(ctx, alpha.ID)
		if err != nil {
			return err
		}
		if summary.Sold != 1 || summary.OnHand() != 0 || summary.LedgerTotal != 0 {
			t.Fatalf("stock summary = %+v", summary)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("check stock: %v", err)
	}

	// The same unit cannot be sold twice, whatever the second document type.
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:        store.DocSalesLease,
		DocDate:        parseDate(t, "2026-10-30"),
		CustomerID:     customer.ID,
		VehicleID:      v.ID,
		PriceCents:     125000000,
		DownCents:      25000000,
		FinanceCompany: "SINGER FINANCE",
	}); !errors.Is(err, store.ErrVehicleUnavailable) {
		t.Fatalf("second sale = %v, want ErrVehicleUnavailable", err)
	}
}

func TestLeasingSaleAndAdvanceReceipt(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}

	leaseVehicle := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000200", 98000000)
	res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:        store.DocSalesLease,
		DocDate:        parseDate(t, "2026-10-30"),
		CustomerID:     customer.ID,
		VehicleID:      leaseVehicle.ID,
		PriceCents:     98000000,
		DownCents:      43000000,
		FinanceCompany: "SINGER FINANCE",
		FinanceAddress: "Mahiyanganaya",
	})
	if err != nil {
		t.Fatalf("leasing sale: %v", err)
	}
	if res.Document.BalanceCents != 55000000 {
		t.Fatalf("balance = %d, want 55000000", res.Document.BalanceCents)
	}
	if res.Document.FinanceCompany != "SINGER FINANCE" {
		t.Fatalf("finance company = %q", res.Document.FinanceCompany)
	}

	advanceVehicle := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000201", 98000000)
	res, err = createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocAdvance,
		DocDate:    parseDate(t, "2026-10-31"),
		CustomerID: customer.ID,
		VehicleID:  advanceVehicle.ID,
		PriceCents: 98000000,
		DownCents:  10000000,
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if res.Document.DocNo != "0001" {
		t.Fatalf("advance number = %q, want 0001 in its own series", res.Document.DocNo)
	}
	if res.VehicleAfter != store.StatusReserved || res.StockMoved {
		t.Fatalf("advance result = %+v", res)
	}

	// An advance must actually be a deposit: zero and the full price are both
	// refused.
	for _, down := range []int64{0, 98000000} {
		if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
			DocType:    store.DocAdvance,
			DocDate:    parseDate(t, "2026-10-31"),
			CustomerID: customer.ID,
			VehicleID:  advanceVehicle.ID,
			PriceCents: 98000000,
			DownCents:  down,
		}); !errors.Is(err, store.ErrInvalidDocument) {
			t.Fatalf("advance with down %d = %v, want ErrInvalidDocument", down, err)
		}
	}
}

func TestQuoteConvertsOnceAndKeepsTheLink(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000300", 125000000)

	quote, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocQuotation,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if quote.Document.Status != store.DocOpen {
		t.Fatalf("quote status = %s, want OPEN", quote.Document.Status)
	}

	invoice, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
		QuoteID:    &quote.Document.ID,
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if invoice.ConvertedQuoteID == nil || *invoice.ConvertedQuoteID != quote.Document.ID {
		t.Fatalf("converted quote id = %v", invoice.ConvertedQuoteID)
	}
	if invoice.Document.QuoteID == nil || *invoice.Document.QuoteID != quote.Document.ID {
		t.Fatalf("invoice quote id = %v", invoice.Document.QuoteID)
	}

	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		after, err := q.DocumentByID(ctx, alpha.ID, quote.Document.ID)
		if err != nil {
			return err
		}
		if after.Status != store.DocConverted {
			t.Fatalf("quote status = %s, want CONVERTED", after.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("check quote: %v", err)
	}

	// The unit is sold now, so a second conversion fails before it can even
	// look at the quote status. Use another unit to prove the quote itself is
	// closed.
	other := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000301", 125000000)
	if _, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  other.ID,
		PriceCents: 125000000,
		QuoteID:    &quote.Document.ID,
	}); !errors.Is(err, store.ErrQuoteNotOpen) {
		t.Fatalf("second conversion = %v, want ErrQuoteNotOpen", err)
	}
}

func TestIssuedDocumentCannotBeEdited(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000400", 125000000)
	res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("sale: %v", err)
	}

	tx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, alpha.ID.String()); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	_, err = tx.Exec(ctx, `update documents set price_cents = 1 where id = $1`, res.Document.ID)
	if sqlStateOf(err) != "DOC01" {
		t.Fatalf("edit issued price = %v, want DOC01", err)
	}
}

func TestConcurrentSalesOnlyOneWins(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000500", 125000000)

	var mu sync.Mutex
	var successes, unavailable, other int
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
				DocType:    store.DocSalesCash,
				DocDate:    parseDate(t, "2026-10-30"),
				CustomerID: customer.ID,
				VehicleID:  v.ID,
				PriceCents: 125000000,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, store.ErrVehicleUnavailable):
				unavailable++
			default:
				other++
			}
		}()
	}
	wg.Wait()

	if successes != 1 || unavailable != 1 || other != 0 {
		t.Fatalf("sales: %d ok, %d unavailable, %d other", successes, unavailable, other)
	}

	var docs int
	err = st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		rows, err := q.ListDocuments(context.Background(), alpha.ID, store.DocumentFilter{})
		docs = len(rows)
		return err
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if docs != 1 {
		t.Fatalf("%d documents, want 1", docs)
	}
}

func TestDocumentIsInvisibleToOtherTenants(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000600", 125000000)
	res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("sale: %v", err)
	}

	err = st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		if _, err := q.DocumentByID(ctx, beta.ID, res.Document.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("beta read alpha's document: %v", err)
		}
		rows, err := q.ListDocuments(ctx, beta.ID, store.DocumentFilter{})
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatalf("beta sees %d documents", len(rows))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("beta: %v", err)
	}
}

func TestDocumentPDFArchivedOnce(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000700", 125000000)
	res, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("sale: %v", err)
	}

	first := bytes.Repeat([]byte("first pdf "), 128)
	second := bytes.Repeat([]byte("second pdf "), 128)

	var stored store.DocumentPDF
	var created bool
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		stored, created, err = q.SaveDocumentPDF(ctx, alpha.ID, res.Document.ID, owner.ID, first)
		return err
	})
	if err != nil {
		t.Fatalf("save first: %v", err)
	}
	if !created {
		t.Fatal("first save did not create a row")
	}
	if stored.SHA256 != sha256Hex(first) {
		t.Fatalf("hash = %s", stored.SHA256)
	}

	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		stored, created, err = q.SaveDocumentPDF(ctx, alpha.ID, res.Document.ID, owner.ID, second)
		return err
	})
	if err != nil {
		t.Fatalf("save second: %v", err)
	}
	if created {
		t.Fatal("second save overwrote the archive")
	}
	if !bytes.Equal(stored.Bytes, first) {
		t.Fatal("archive changed after a second render")
	}

	err = st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		_, _, err := q.SaveDocumentPDF(ctx, beta.ID, res.Document.ID, owner.ID, first)
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("beta saved a pdf for alpha's document: %v", err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
