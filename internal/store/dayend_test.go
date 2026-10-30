package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// todayWindow is the business day the test is running in, with the instants
// the stock ledger is filtered by. Stock movements are written by the sale, so
// they are stamped now and only fall inside today's window.
func todayWindow(t *testing.T) store.DayWindow {
	t.Helper()
	now := time.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return store.DayWindow{Date: day, Start: day, End: day.AddDate(0, 0, 1)}
}

func closeDay(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, window store.DayWindow, float, counted int64) (store.DayEnd, error) {
	t.Helper()
	var end store.DayEnd
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		end, err = q.CloseDay(context.Background(), tenantID, actorID, store.DayEndParams{
			Window:            window,
			OpeningFloatCents: float,
			CountedCents:      counted,
		})
		return err
	})
	return end, err
}

func TestCloseDayStoresTheVariance(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	window := todayWindow(t)

	// One leasing sale today, with 5,000,000 down left to settle.
	doc := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000003001", 100000000, 40000000, window.Date)

	cash := cashPayment(doc.ID, 25000000)
	cash.PaidOn = window.Date
	if _, err := postPayment(t, st, alpha.ID, owner.ID, cash); err != nil {
		t.Fatalf("cash payment: %v", err)
	}
	bank := cashPayment(doc.ID, 10000000)
	bank.PaidOn = window.Date
	bank.Method = store.MethodBank
	if _, err := postPayment(t, st, alpha.ID, owner.ID, bank); err != nil {
		t.Fatalf("bank payment: %v", err)
	}

	// The preview the cashier sees before closing must be the totals that get
	// stored, not a second calculation.
	var preview store.DayTotals
	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		preview, err = q.DayTotalsFor(ctx, alpha.ID, window)
		return err
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.CashInCents != 25000000 || preview.CashOutCents != 0 {
		t.Fatalf("preview cash = in %d out %d", preview.CashInCents, preview.CashOutCents)
	}
	if preview.PaymentsTotalCents != 35000000 {
		t.Fatalf("preview payments = %d", preview.PaymentsTotalCents)
	}
	if preview.SalesCount != 1 || preview.SalesTotalCents != 100000000 {
		t.Fatalf("preview sales = %d / %d", preview.SalesCount, preview.SalesTotalCents)
	}

	// Float 50,000.00 plus 250,000.00 cash taken is 300,000.00 expected; the
	// cashier counts 295,000.00.
	end, err := closeDay(t, st, alpha.ID, owner.ID, window, 5000000, 29500000)
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	if end.ZNo != "0001" || end.ZYear != window.Date.Year() {
		t.Fatalf("z number = %s/%d", end.ZNo, end.ZYear)
	}
	if end.ExpectedCents != 30000000 {
		t.Fatalf("expected = %d, want 30000000", end.ExpectedCents)
	}
	if end.VarianceCents != -500000 {
		t.Fatalf("variance = %d, want -500000", end.VarianceCents)
	}
	if !end.Short() {
		t.Fatal("a negative variance must read as short")
	}
	if end.CashInCents != 25000000 || end.CashOutCents != 0 {
		t.Fatalf("cash = in %d out %d", end.CashInCents, end.CashOutCents)
	}
	if end.SalesCount != 1 || end.SalesTotalCents != 100000000 || end.PaymentsTotalCents != 35000000 {
		t.Fatalf("totals = %+v", end)
	}
	if end.ClosedByName == nil || *end.ClosedByName != "Test User" {
		t.Fatalf("closed by = %v", end.ClosedByName)
	}

	// The lines are the paper: payments by method, documents by type, stock by
	// kind. They are stored, not recomputed later.
	found := make(map[string]store.DayEndLine)
	for _, line := range end.Lines {
		found[line.Section+"/"+line.Label] = line
	}
	for _, want := range []struct {
		key    string
		qty    int
		amount int64
	}{
		{store.DayPayments + "/CASH", 1, 25000000},
		{store.DayPayments + "/BANK", 1, 10000000},
		{store.DayDocuments + "/SALES_LEASING", 1, 100000000},
		{store.DayStock + "/SELL", 1, 0},
	} {
		line, ok := found[want.key]
		if !ok {
			t.Fatalf("no %s line in %+v", want.key, end.Lines)
		}
		if line.Qty != want.qty || line.AmountCents != want.amount {
			t.Fatalf("%s = %d / %d, want %d / %d", want.key, line.Qty, line.AmountCents, want.qty, want.amount)
		}
	}

	// Closing again on the same date is refused.
	if _, err := closeDay(t, st, alpha.ID, owner.ID, window, 0, 0); !errors.Is(err, store.ErrDayAlreadyClosed) {
		t.Fatalf("second close = %v, want ErrDayAlreadyClosed", err)
	}

	// The day now reads as closed, and the next day opens on what was counted.
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		closed, err := q.DayEndOnDate(ctx, alpha.ID, window.Date)
		if err != nil {
			return err
		}
		if closed.ID != end.ID {
			t.Fatalf("closed day = %s, want %s", closed.ID, end.ID)
		}
		counted, err := q.LastCountedCash(ctx, alpha.ID)
		if err != nil {
			return err
		}
		if counted != 29500000 {
			t.Fatalf("last counted = %d", counted)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
}

func TestOpenDayHasNoClose(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	window := todayWindow(t)

	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		if _, err := q.DayEndOnDate(ctx, alpha.ID, window.Date); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("open day = %v, want ErrNotFound", err)
		}
		counted, err := q.LastCountedCash(ctx, alpha.ID)
		if err != nil {
			return err
		}
		if counted != 0 {
			t.Fatalf("first day float suggestion = %d, want 0", counted)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("open day: %v", err)
	}

	// A day with nothing on it still closes, and its variance is the drawer.
	end, err := closeDay(t, st, alpha.ID, owner.ID, window, 100000, 100000)
	if err != nil {
		t.Fatalf("close empty day: %v", err)
	}
	if end.ExpectedCents != 100000 || end.VarianceCents != 0 || len(end.Lines) != 0 {
		t.Fatalf("empty day = %+v", end)
	}
}

func TestClosedDayCannotBeChanged(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	window := todayWindow(t)

	// A day with real paper on it, so the frozen lines are worth protecting.
	doc := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000003201", 100000000, 40000000, window.Date)
	pay := cashPayment(doc.ID, 25000000)
	pay.PaidOn = window.Date
	if _, err := postPayment(t, st, alpha.ID, owner.ID, pay); err != nil {
		t.Fatalf("payment: %v", err)
	}

	end, err := closeDay(t, st, alpha.ID, owner.ID, window, 0, 25000000)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(end.Lines) == 0 {
		t.Fatal("the closed day has no frozen lines to test")
	}

	// The runtime role is not even granted the statement.
	tx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, alpha.ID.String()); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := tx.Exec(ctx, `update day_ends set counted_cents = 0 where id = $1`, end.ID); err == nil {
		t.Fatal("app_rw updated a closed day")
	} else if got := sqlStateOf(err); got != "42501" && got != "DAY01" {
		t.Fatalf("update error = %v (%s), want a refusal", err, got)
	}
	if _, err := tx.Exec(ctx, `delete from day_ends where id = $1`, end.ID); err == nil {
		t.Fatal("app_rw deleted a closed day")
	}

	// The owner role could try, and the trigger stops it there too.
	if _, err := admin.Exec(ctx, `update day_ends set counted_cents = 0 where id = $1`, end.ID); sqlStateOf(err) != "DAY01" {
		t.Fatalf("owner update = %v (%s), want DAY01", err, sqlStateOf(err))
	}
	if _, err := admin.Exec(ctx, `update day_end_lines set qty = 9 where day_end_id = $1`, end.ID); sqlStateOf(err) != "DAY01" {
		t.Fatalf("owner line update = %v (%s), want DAY01", err, sqlStateOf(err))
	}
	if _, err := admin.Exec(ctx, `delete from day_ends where id = $1`, end.ID); sqlStateOf(err) != "DAY01" {
		t.Fatalf("owner delete = %v (%s), want DAY01", err, sqlStateOf(err))
	}
}

func TestReversalOnALaterDayLandsOnThatDay(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	manager := newUser(t, st, alpha.ID, "manager@alpha.test", "MANAGER")
	day := todayWindow(t)
	nextDay := store.DayWindow{
		Date:  day.Date.AddDate(0, 0, 1),
		Start: day.Start.AddDate(0, 0, 1),
		End:   day.End.AddDate(0, 0, 1),
	}

	doc := leasedSaleOn(t, st, alpha.ID, owner.ID, "ZAPM0000003101", 100000000, 40000000, day.Date)
	pay := cashPayment(doc.ID, 25000000)
	pay.PaidOn = day.Date
	receipt, err := postPayment(t, st, alpha.ID, owner.ID, pay)
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	// Day one: 250,000.00 cash in the drawer, counted exactly.
	today, err := closeDay(t, st, alpha.ID, owner.ID, day, 0, 25000000)
	if err != nil {
		t.Fatalf("close day one: %v", err)
	}
	if today.CashInCents != 25000000 || today.VarianceCents != 0 {
		t.Fatalf("day one = %+v", today)
	}

	// Day two: the receipt is reversed. The money leaves the drawer today, and
	// today's z says so; day one's paper does not change.
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		_, err := q.ReversePayment(ctx, alpha.ID, receipt.ID, manager.ID, "customer cancelled the sale", nextDay.Date)
		return err
	})
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}

	next, err := closeDay(t, st, alpha.ID, manager.ID, nextDay, 25000000, 0)
	if err != nil {
		t.Fatalf("close day two: %v", err)
	}
	if next.CashInCents != 0 || next.CashOutCents != 25000000 {
		t.Fatalf("day two cash = in %d out %d", next.CashInCents, next.CashOutCents)
	}
	if next.ExpectedCents != 0 || next.VarianceCents != 0 {
		t.Fatalf("day two = expected %d variance %d", next.ExpectedCents, next.VarianceCents)
	}

	reversalLines := 0
	for _, line := range next.Lines {
		if line.Section == store.DayReversals && line.Label == store.MethodCash {
			reversalLines++
			if line.AmountCents != 25000000 {
				t.Fatalf("reversal line = %d", line.AmountCents)
			}
		}
	}
	if reversalLines != 1 {
		t.Fatalf("day two has %d cash reversal lines", reversalLines)
	}

	// Day one is untouched: it still says the cash was taken and counted.
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		again, err := q.DayEndByID(ctx, alpha.ID, today.ID)
		if err != nil {
			return err
		}
		if again.CountedCents != 25000000 || again.CashInCents != 25000000 || again.VarianceCents != 0 {
			t.Fatalf("day one changed: %+v", again)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("day one: %v", err)
	}
}

func TestDayEndIsInvisibleToOtherTenants(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	ownerAlpha := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	ownerBeta := newUser(t, st, beta.ID, "owner@beta.test", "OWNER")
	window := todayWindow(t)

	end, err := closeDay(t, st, alpha.ID, ownerAlpha.ID, window, 0, 1000)
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	err = st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		if _, err := q.DayEndByID(ctx, beta.ID, end.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("beta read alpha's day end: %v", err)
		}
		if _, err := q.DayEndOnDate(ctx, beta.ID, window.Date); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("beta found a day end on the same date: %v", err)
		}
		rows, err := q.ListDayEnds(ctx, beta.ID, 10)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatalf("beta sees %d day ends", len(rows))
		}
		// Beta's own day closes on its own number.
		own, err := q.CloseDay(ctx, beta.ID, ownerBeta.ID, store.DayEndParams{
			Window: window, OpeningFloatCents: 0, CountedCents: 0,
		})
		if err != nil {
			return err
		}
		if own.ZNo != "0001" {
			t.Fatalf("beta z number = %s, want 0001", own.ZNo)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("beta: %v", err)
	}
}
