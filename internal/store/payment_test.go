package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// leasedSale issues a leasing invoice with a balance left to settle, which is
// the only document a receipt can pay down.
func leasedSale(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, chassis string, price, down int64) store.Document {
	return leasedSaleOn(t, st, tenantID, actorID, chassis, price, down, parseDate(t, "2026-10-30"))
}

func leasedSaleOn(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, chassis string, price, down int64, docDate time.Time) store.Document {
	t.Helper()
	customer, err := newCustomer(t, st, tenantID, actorID, "Saman Perera", "NIC-"+chassis[len(chassis)-4:])
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, tenantID, actorID, chassis, price)
	res, err := createDocument(t, st, tenantID, actorID, store.DocumentParams{
		DocType:        store.DocSalesLease,
		DocDate:        docDate,
		CustomerID:     customer.ID,
		VehicleID:      v.ID,
		PriceCents:     price,
		DownCents:      down,
		FinanceCompany: "SINGER FINANCE",
		// These tests predate approvals, so they declare their own business
		// day: the document is dated the day it is issued, not backdated.
		Today: docDate,
	})
	if err != nil {
		t.Fatalf("leasing sale: %v", err)
	}
	return res.Document
}

func postPayment(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, p store.PaymentParams) (store.Payment, error) {
	t.Helper()
	var pay store.Payment
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		pay, err = q.CreatePayment(context.Background(), tenantID, actorID, p)
		return err
	})
	return pay, err
}

func cashPayment(documentID uuid.UUID, amount int64) store.PaymentParams {
	return store.PaymentParams{
		DocumentID:  documentID,
		PaidOn:      parseDateValue("2026-10-30"),
		Method:      store.MethodCash,
		AmountCents: amount,
	}
}

func parseDateValue(value string) time.Time {
	at, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return at
}

func TestPaymentCannotExceedTheOutstandingBalance(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	// 1,000,000 price, 400,000 down: 600,000 is owed.
	doc := leasedSale(t, st, alpha.ID, owner.ID, "ZAPM0000002001", 100000000, 40000000)

	first, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 25000000))
	if err != nil {
		t.Fatalf("first payment: %v", err)
	}
	if first.ReceiptNo != "0001" || first.Status != store.PaymentPosted {
		t.Fatalf("receipt = %s %s", first.ReceiptNo, first.Status)
	}
	if first.AmountCents != 25000000 || first.DocNo != doc.DocNo || first.CustomerName != "Saman Perera" {
		t.Fatalf("receipt = %+v", first)
	}

	// One rupee more than what is left is refused.
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 35000001)); !errors.Is(err, store.ErrPaymentOverBalance) {
		t.Fatalf("overpayment = %v, want ErrPaymentOverBalance", err)
	}

	// The refusal did not burn a number: the next receipt is 0002.
	second, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 35000000))
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if second.ReceiptNo != "0002" {
		t.Fatalf("settling receipt = %s, want 0002", second.ReceiptNo)
	}

	// Nothing left, so even one rupee is too much.
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 1)); !errors.Is(err, store.ErrPaymentOverBalance) {
		t.Fatalf("payment on a settled invoice = %v, want ErrPaymentOverBalance", err)
	}

	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		balance, err := q.DocumentBalanceFor(ctx, alpha.ID, doc.ID)
		if err != nil {
			return err
		}
		if balance.PaidCents != 60000000 || balance.OutstandingCents != 0 {
			t.Fatalf("balance = %+v", balance)
		}
		// The invoice itself is untouched: what was printed still stands.
		again, err := q.DocumentByID(ctx, alpha.ID, doc.ID)
		if err != nil {
			return err
		}
		if again.BalanceCents != doc.BalanceCents || again.PriceCents != doc.PriceCents {
			t.Fatalf("invoice was edited: %+v", again)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
}

func TestConcurrentPaymentsCannotOverpay(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	doc := leasedSale(t, st, alpha.ID, owner.ID, "ZAPM0000002101", 100000000, 40000000)

	// Two clerks each post the whole outstanding balance at the same moment.
	var mu sync.Mutex
	var accepted, refused, other int
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 60000000))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, store.ErrPaymentOverBalance):
				refused++
			default:
				other++
			}
		}()
	}
	wg.Wait()

	if accepted != 1 || refused != 1 || other != 0 {
		t.Fatalf("payments: %d accepted, %d refused, %d other", accepted, refused, other)
	}
}

func TestReversalFreesTheBalanceAndKeepsTheRecord(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	manager := newUser(t, st, alpha.ID, "manager@alpha.test", "MANAGER")
	doc := leasedSale(t, st, alpha.ID, owner.ID, "ZAPM0000002201", 100000000, 40000000)

	pay, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 25000000))
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	reversedOn := parseDateValue("2026-10-31")
	var reversed store.Payment
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		reversed, err = q.ReversePayment(ctx, alpha.ID, pay.ID, manager.ID, "receipt was for the wrong invoice", reversedOn)
		return err
	})
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if reversed.Status != store.PaymentReversed {
		t.Fatalf("status = %s, want REVERSED", reversed.Status)
	}
	if reversed.ReversedOn == nil || !reversed.ReversedOn.Equal(reversedOn) {
		t.Fatalf("reversed on = %v", reversed.ReversedOn)
	}
	if reversed.ReversalReason != "receipt was for the wrong invoice" {
		t.Fatalf("reason = %q", reversed.ReversalReason)
	}
	if reversed.ReversedByName == nil || *reversed.ReversedByName != "Test User" {
		t.Fatalf("reversed by = %v", reversed.ReversedByName)
	}
	if reversed.AmountCents != 25000000 {
		t.Fatalf("reversal changed the amount: %d", reversed.AmountCents)
	}

	// A receipt is reversed once.
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		_, err := q.ReversePayment(ctx, alpha.ID, pay.ID, manager.ID, "again", reversedOn)
		return err
	})
	if !errors.Is(err, store.ErrPaymentNotReversible) {
		t.Fatalf("second reversal = %v, want ErrPaymentNotReversible", err)
	}

	// The balance is owed again, and the same amount can be taken once more.
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		balance, err := q.DocumentBalanceFor(ctx, alpha.ID, doc.ID)
		if err != nil {
			return err
		}
		if balance.PaidCents != 0 || balance.OutstandingCents != 60000000 {
			t.Fatalf("balance after reversal = %+v", balance)
		}
		rows, err := q.ListPayments(ctx, alpha.ID, store.PaymentFilter{})
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Status != store.PaymentReversed {
			t.Fatalf("payments = %+v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("after reversal: %v", err)
	}

	if _, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 25000000)); err != nil {
		t.Fatalf("repay: %v", err)
	}
}

func TestPaymentNeedsAReasonToReverse(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	doc := leasedSale(t, st, alpha.ID, owner.ID, "ZAPM0000002301", 100000000, 40000000)
	pay, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(doc.ID, 100000))
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	for _, reason := range []string{"", "  ", "no"} {
		err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
			_, err := q.ReversePayment(context.Background(), alpha.ID, pay.ID, owner.ID, reason, parseDateValue("2026-10-31"))
			return err
		})
		if !errors.Is(err, store.ErrInvalidPayment) {
			t.Fatalf("reason %q = %v, want ErrInvalidPayment", reason, err)
		}
	}
}

func TestOnlyAnInvoiceTakesAPayment(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	v := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000002401", 125000000)

	// A quotation offers a price; it is not owed anything yet.
	quote, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocQuotation,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("quotation: %v", err)
	}
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(quote.Document.ID, 1000)); !errors.Is(err, store.ErrInvalidPayment) {
		t.Fatalf("payment on a quotation = %v, want ErrInvalidPayment", err)
	}

	// A cash invoice is paid in full at issue, so a receipt against it would be
	// an overpayment, not a valid payment.
	cash, err := createDocument(t, st, alpha.ID, owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-30"),
		CustomerID: customer.ID,
		VehicleID:  v.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("cash sale: %v", err)
	}
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cashPayment(cash.Document.ID, 1000)); !errors.Is(err, store.ErrPaymentOverBalance) {
		t.Fatalf("payment on a cash invoice = %v, want ErrPaymentOverBalance", err)
	}

	// Nothing at all cannot be paid either.
	if _, err := postPayment(t, st, alpha.ID, owner.ID, store.PaymentParams{
		DocumentID:  cash.Document.ID,
		PaidOn:      parseDate(t, "2026-10-30"),
		Method:      "CRYPTO",
		AmountCents: 1000,
	}); !errors.Is(err, store.ErrInvalidPayment) {
		t.Fatalf("unknown method = %v, want ErrInvalidPayment", err)
	}
}

func TestReceiptsAreNumberedPerTenantAndYear(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	ownerAlpha := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	ownerBeta := newUser(t, st, beta.ID, "owner@beta.test", "OWNER")

	alphaDoc := leasedSale(t, st, alpha.ID, ownerAlpha.ID, "ZAPM0000002501", 100000000, 40000000)
	betaDoc := leasedSale(t, st, beta.ID, ownerBeta.ID, "ZAPM0000002502", 100000000, 40000000)

	first, err := postPayment(t, st, alpha.ID, ownerAlpha.ID, cashPayment(alphaDoc.ID, 1000))
	if err != nil {
		t.Fatalf("alpha first: %v", err)
	}
	if first.ReceiptNo != "0001" {
		t.Fatalf("alpha first receipt = %s", first.ReceiptNo)
	}
	second, err := postPayment(t, st, alpha.ID, ownerAlpha.ID, cashPayment(alphaDoc.ID, 1000))
	if err != nil {
		t.Fatalf("alpha second: %v", err)
	}
	if second.ReceiptNo != "0002" {
		t.Fatalf("alpha second receipt = %s", second.ReceiptNo)
	}

	// The receipt series is separate from the invoice series, and beta starts
	// its own.
	betaFirst, err := postPayment(t, st, beta.ID, ownerBeta.ID, cashPayment(betaDoc.ID, 1000))
	if err != nil {
		t.Fatalf("beta first: %v", err)
	}
	if betaFirst.ReceiptNo != "0001" {
		t.Fatalf("beta first receipt = %s", betaFirst.ReceiptNo)
	}
	if betaFirst.DocNo != betaDoc.DocNo {
		t.Fatalf("beta receipt points at %s", betaFirst.DocNo)
	}
}

func TestPaymentIsInvisibleToOtherTenants(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	ownerAlpha := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	ownerBeta := newUser(t, st, beta.ID, "owner@beta.test", "OWNER")

	doc := leasedSale(t, st, alpha.ID, ownerAlpha.ID, "ZAPM0000002601", 100000000, 40000000)
	pay, err := postPayment(t, st, alpha.ID, ownerAlpha.ID, cashPayment(doc.ID, 1000))
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	err = st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		if _, err := q.PaymentByID(ctx, beta.ID, pay.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("beta read alpha's receipt: %v", err)
		}
		rows, err := q.ListPayments(ctx, beta.ID, store.PaymentFilter{})
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatalf("beta sees %d receipts", len(rows))
		}
		if _, err := q.CreatePayment(ctx, beta.ID, ownerBeta.ID, cashPayment(doc.ID, 1000)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("beta paid alpha's invoice: %v", err)
		}
		if _, err := q.DocumentBalanceFor(ctx, beta.ID, doc.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("beta read alpha's balance: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("beta: %v", err)
	}
}

// reversePayment hands a receipt back on the given business date.
func reversePayment(t *testing.T, st *store.Store, tenantID, actorID, paymentID uuid.UUID, reason string, on time.Time) (store.Payment, error) {
	t.Helper()
	var pay store.Payment
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		pay, err = q.ReversePayment(context.Background(), tenantID, paymentID, actorID, reason, on)
		return err
	})
	return pay, err
}

func TestMoneyCannotBeDatedOnAClosedDay(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	window := todayWindow(t)

	// Yesterday's invoice and a receipt for it, so a reversal has something to
	// withdraw today.
	yesterday := window.Date.AddDate(0, 0, -1)
	doc := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000002401", 100000000, 40000000, yesterday)
	earlyParams := cashPayment(doc.ID, 25000000)
	earlyParams.PaidOn = yesterday
	early, err := postPayment(t, st, alpha.ID, owner.ID, earlyParams)
	if err != nil {
		t.Fatalf("yesterday's payment: %v", err)
	}

	// The cashier counts the drawer and closes today.
	if _, err := closeDay(t, st, alpha.ID, owner.ID, window, 0, 0); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Cash handed over after the z cannot be dated today, because the day it
	// would land on is already counted.
	late := cashPayment(doc.ID, 10000000)
	late.PaidOn = window.Date
	if _, err := postPayment(t, st, alpha.ID, owner.ID, late); !errors.Is(err, store.ErrDayAlreadyClosed) {
		t.Fatalf("payment on a closed day = %v, want ErrDayAlreadyClosed", err)
	}

	// Neither can the money be handed back on a closed day, for the same
	// reason: the reversal would sit on a date that is already signed.
	if _, err := reversePayment(t, st, alpha.ID, owner.ID, early.ID, "wrong amount", window.Date); !errors.Is(err, store.ErrDayAlreadyClosed) {
		t.Fatalf("reversal on a closed day = %v, want ErrDayAlreadyClosed", err)
	}

	// The refusal burnt no receipt number. Yesterday's receipt was 0001 in this
	// year's series, so the next one to be issued is 0002: the two refused
	// attempts took nothing.
	tomorrow := window.Date.AddDate(0, 0, 1)
	next := cashPayment(doc.ID, 10000000)
	next.PaidOn = tomorrow
	pay, err := postPayment(t, st, alpha.ID, owner.ID, next)
	if err != nil {
		t.Fatalf("payment on the next day: %v", err)
	}
	if early.ReceiptNo != "0001" {
		t.Fatalf("yesterday's receipt = %q, want 0001", early.ReceiptNo)
	}
	if pay.ReceiptNo != "0002" {
		t.Fatalf("receipt after two refusals = %q, want 0002", pay.ReceiptNo)
	}
}
