package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// serialDay is a fixed business date. The year and the month both appear in a
// serial, so an expected one can only be written out in full against a fixed
// day.
var serialDay = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

// newSluggedTenant creates a tenant whose slug is known, so the branch segment
// of its serials can be predicted. The shared helper picks a random slug to
// keep tenant names from colliding, which is right for every other test.
func newSluggedTenant(t *testing.T, st *store.Store, slug, name, domain string) store.Tenant {
	t.Helper()
	tn, err := st.CreateTenant(context.Background(), slug, name, domain)
	if err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	return tn
}

func TestSerialFormatFollowsTheIRDShape(t *testing.T) {
	got := store.FormatSerial(serialDay, "TANG", 41)
	if want := "26NOV_TANG_00041"; got != want {
		t.Fatalf("serial = %q, want %q", got, want)
	}
	if len(got) > 40 {
		t.Errorf("serial is %d characters, the IRD allows at most 40", len(got))
	}
	if strings.ContainsAny(got, " \t") {
		t.Errorf("serial %q contains a space", got)
	}

	// The counter is the last segment and widens rather than wrapping, so a
	// series that outgrows five digits keeps reading as one number.
	if got := store.FormatSerial(serialDay, "TANG", 100000); !strings.HasSuffix(got, "_100000") {
		t.Errorf("serial past 99999 = %q, want the counter intact", got)
	}
}

func TestSalesDocumentTypesAreNotSeries(t *testing.T) {
	// A caller that forgets SeriesFor has to be refused, or it opens a second
	// sales counter beside the real one and two papers share a number.
	if store.ValidSeries(store.DocSalesCash) || store.ValidSeries(store.DocSalesLease) {
		t.Fatal("a sales document type is still accepted as a series")
	}
	if !store.ValidSeries(store.SeriesSales) {
		t.Fatal("the sales series is not accepted")
	}
	for _, docType := range []string{store.DocSalesCash, store.DocSalesLease} {
		if got := store.SeriesFor(docType); got != store.SeriesSales {
			t.Errorf("SeriesFor(%s) = %s, want %s", docType, got, store.SeriesSales)
		}
	}
	if got := store.SeriesFor(store.DocProforma); got != store.DocProforma {
		t.Errorf("SeriesFor(PROFORMA) = %s, want its own series", got)
	}
}

// TestCashAndLeasingShareOneSalesSeries is the point of D26. The customer reads
// "SALES INVOICE" on both kinds, and the app this replaces spent one number
// space for both, so a leasing sale must not restart at 1 beside a cash one.
func TestCashAndLeasingShareOneSalesSeries(t *testing.T) {
	st := freshStore(t)
	alpha := newSluggedTenant(t, st, "alpha-motors", "Alpha Motors", "alpha.test")
	ctx := context.Background()

	next := func(docType string) string {
		t.Helper()
		var no string
		err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
			var err error
			no, err = q.AllocateNo(ctx, alpha.ID, store.SeriesFor(docType), serialDay)
			return err
		})
		if err != nil {
			t.Fatalf("allocate %s: %v", docType, err)
		}
		return no
	}

	if got, want := next(store.DocSalesCash), store.FormatSerial(serialDay, "ALPH", 1); got != want {
		t.Fatalf("first cash invoice = %q, want %q", got, want)
	}
	if got, want := next(store.DocSalesLease), store.FormatSerial(serialDay, "ALPH", 2); got != want {
		t.Fatalf("first leasing invoice = %q, want %q (one series, not a restart)", got, want)
	}
}

func TestDocumentNumbersAreUniqueUnderConcurrency(t *testing.T) {
	st := freshStore(t)
	alpha := newSluggedTenant(t, st, "alpha-motors", "Alpha Motors", "alpha.test")
	ctx := context.Background()

	const workers = 40
	var mu sync.Mutex
	numbers := make(map[string]int, workers)
	var firstErr error
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
				no, err := q.AllocateNo(ctx, alpha.ID, store.SeriesSales, serialDay)
				if err != nil {
					return err
				}
				mu.Lock()
				numbers[no]++
				mu.Unlock()
				return nil
			})
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("allocate: %v", firstErr)
	}
	if len(numbers) != workers {
		t.Fatalf("got %d distinct numbers, want %d: %v", len(numbers), workers, numbers)
	}
	for i := 1; i <= workers; i++ {
		want := store.FormatSerial(serialDay, "ALPH", int64(i))
		if numbers[want] != 1 {
			t.Errorf("number %s issued %d times, want once", want, numbers[want])
		}
	}
}

func TestDocumentNumberRollsBackWithTheDocument(t *testing.T) {
	st := freshStore(t)
	alpha := newSluggedTenant(t, st, "alpha-motors", "Alpha Motors", "alpha.test")
	newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	ctx := context.Background()

	// A transaction that takes a number and then fails must give it back, so
	// the next document still gets the first number and the series has no hole.
	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		if _, err := q.AllocateNo(ctx, alpha.ID, store.SeriesSales, serialDay); err != nil {
			return err
		}
		return errors.New("clerk cancelled")
	})
	if err == nil || err.Error() != "clerk cancelled" {
		t.Fatalf("transaction = %v, want the deliberate failure", err)
	}

	var no string
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		no, err = q.AllocateNo(ctx, alpha.ID, store.SeriesSales, serialDay)
		return err
	})
	if err != nil {
		t.Fatalf("allocate after rollback: %v", err)
	}
	if want := store.FormatSerial(serialDay, "ALPH", 1); no != want {
		t.Fatalf("number after rollback = %q, want %q", no, want)
	}
}

func TestDocumentSeriesAreSeparatePerTypeYearAndTenant(t *testing.T) {
	st := freshStore(t)
	alpha := newSluggedTenant(t, st, "alpha-motors", "Alpha Motors", "alpha.test")
	beta := newSluggedTenant(t, st, "beta-motors", "Beta Motors", "beta.test")
	ctx := context.Background()

	allocate := func(tenantID uuid.UUID, series string, day time.Time) string {
		t.Helper()
		var no string
		err := st.InTenant(ctx, tenantID, func(q *store.Store) error {
			var err error
			no, err = q.AllocateNo(ctx, tenantID, series, day)
			return err
		})
		if err != nil {
			t.Fatalf("allocate %s: %v", series, err)
		}
		return no
	}

	// Alpha, the sales series, 2026.
	if got, want := allocate(alpha.ID, store.SeriesSales, serialDay), store.FormatSerial(serialDay, "ALPH", 1); got != want {
		t.Fatalf("first sale 2026 = %q, want %q", got, want)
	}
	// A second document type starts its own series.
	if got, want := allocate(alpha.ID, store.DocQuotation, serialDay), store.FormatSerial(serialDay, "ALPH", 1); got != want {
		t.Fatalf("first quotation = %q, want %q", got, want)
	}
	// A new year starts its own series.
	nextYear := time.Date(2027, 1, 4, 9, 0, 0, 0, time.UTC)
	if got, want := allocate(alpha.ID, store.SeriesSales, nextYear), store.FormatSerial(nextYear, "ALPH", 1); got != want {
		t.Fatalf("first sale 2027 = %q, want %q", got, want)
	}
	// And another tenant starts its own, under its own branch segment.
	if got, want := allocate(beta.ID, store.SeriesSales, serialDay), store.FormatSerial(serialDay, "BETA", 1); got != want {
		t.Fatalf("beta first sale = %q, want %q", got, want)
	}
}

func TestSerialCarriesTheMonthOfThePaperNotOfTheClock(t *testing.T) {
	st := freshStore(t)
	alpha := newSluggedTenant(t, st, "alpha-motors", "Alpha Motors", "alpha.test")
	ctx := context.Background()

	// A backdated paper is numbered from the same yearly counter but carries
	// the month it is dated, which is what has to agree with the date printed
	// beside it.
	day := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	var no string
	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		no, err = q.AllocateNo(ctx, alpha.ID, store.SeriesSales, day)
		return err
	})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if want := fmt.Sprintf("26MAR_ALPH_00001"); no != want {
		t.Fatalf("backdated serial = %q, want %q", no, want)
	}
}
