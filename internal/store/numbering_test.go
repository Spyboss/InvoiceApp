package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

func TestDocumentNumbersAreUniqueUnderConcurrency(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
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
				no, err := q.AllocateNo(ctx, alpha.ID, store.DocSalesCash, 2026)
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
		want := fmt.Sprintf("%04d", i)
		if numbers[want] != 1 {
			t.Errorf("number %s issued %d times, want once", want, numbers[want])
		}
	}
}

func TestDocumentNumberRollsBackWithTheDocument(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	ctx := context.Background()

	// A transaction that takes a number and then fails must give it back, so
	// the next document still gets 0001 and the series has no hole.
	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		if _, err := q.AllocateNo(ctx, alpha.ID, store.DocSalesCash, 2026); err != nil {
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
		no, err = q.AllocateNo(ctx, alpha.ID, store.DocSalesCash, 2026)
		return err
	})
	if err != nil {
		t.Fatalf("allocate after rollback: %v", err)
	}
	if no != "0001" {
		t.Fatalf("number after rollback = %q, want 0001", no)
	}
}

func TestDocumentSeriesAreSeparatePerTypeYearAndTenant(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	ctx := context.Background()

	allocate := func(tenantID uuid.UUID, docType string, year int) string {
		t.Helper()
		var no string
		err := st.InTenant(ctx, tenantID, func(q *store.Store) error {
			var err error
			no, err = q.AllocateNo(ctx, tenantID, docType, year)
			return err
		})
		if err != nil {
			t.Fatalf("allocate %s %d: %v", docType, year, err)
		}
		return no
	}

	// Alpha, cash, 2026.
	if got := allocate(alpha.ID, store.DocSalesCash, 2026); got != "0001" {
		t.Fatalf("first cash 2026 = %q", got)
	}
	// A second type starts its own series.
	if got := allocate(alpha.ID, store.DocSalesLease, 2026); got != "0001" {
		t.Fatalf("first leasing = %q", got)
	}
	// A new year starts its own series.
	if got := allocate(alpha.ID, store.DocSalesCash, 2027); got != "0001" {
		t.Fatalf("first cash 2027 = %q", got)
	}
	// Another tenant starts its own series.
	var betaNo string
	err := st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		var err error
		betaNo, err = q.AllocateNo(ctx, beta.ID, store.DocSalesCash, 2026)
		return err
	})
	if err != nil {
		t.Fatalf("beta allocate: %v", err)
	}
	if betaNo != "0001" {
		t.Fatalf("beta first cash = %q, want 0001", betaNo)
	}
}
