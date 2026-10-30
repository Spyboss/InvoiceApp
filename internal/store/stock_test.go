package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/spyboss/invoiceapp/internal/store"
)

func newCustomer(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, name, nic string) (store.Customer, error) {
	t.Helper()
	var c store.Customer
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		c, err = q.CreateCustomer(context.Background(), tenantID, actorID, store.CustomerParams{
			Name:  name,
			NIC:   nic,
			Phone: "0771234567",
		})
		return err
	})
	return c, err
}

func newVehicle(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, chassis string) store.Vehicle {
	t.Helper()
	var v store.Vehicle
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		v, err = q.CreateVehicle(context.Background(), tenantID, actorID, store.VehicleParams{
			Make:       "Piaggio",
			Model:      "APE City",
			ChassisNo:  chassis,
			PriceCents: 125000000,
			ReceivedAt: parseDate(t, "2026-10-30"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("create vehicle: %v", err)
	}
	return v
}

func TestCustomerNICUniquePerTenant(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	if _, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V"); err != nil {
		t.Fatalf("first customer: %v", err)
	}

	// The index is on upper(nic), so a lower case letter cannot slip a
	// duplicate past it.
	if _, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman P.", "907654321v"); !errors.Is(err, store.ErrDuplicateNIC) {
		t.Fatalf("duplicate nic = %v, want ErrDuplicateNIC", err)
	}

	// The same person at another dealership is a different customer.
	if _, err := newCustomer(t, st, beta.ID, owner.ID, "Saman Perera", "907654321V"); err != nil {
		t.Fatalf("same nic other tenant: %v", err)
	}
}

func TestCustomerIsInvisibleToOtherTenants(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	c, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	err = st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		if _, err := q.CustomerByID(ctx, beta.ID, c.ID); err == nil {
			t.Fatal("beta read alpha's customer")
		}
		rows, err := q.ListCustomers(ctx, beta.ID, "", 10)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatalf("beta sees %d customers, want 0", len(rows))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("beta lookup: %v", err)
	}
}

func TestChassisUniquePerTenant(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	newVehicle(t, st, alpha.ID, owner.ID, "ZAPM1234567890")

	err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		_, err := q.CreateVehicle(context.Background(), alpha.ID, owner.ID, store.VehicleParams{
			Make:       "Piaggio",
			Model:      "APE City",
			ChassisNo:  "zapm1234567890",
			ReceivedAt: parseDate(t, "2026-10-30"),
		})
		return err
	})
	if !errors.Is(err, store.ErrDuplicateChassis) {
		t.Fatalf("duplicate chassis = %v, want ErrDuplicateChassis", err)
	}

	newVehicle(t, st, beta.ID, owner.ID, "ZAPM1234567890")
}

func TestStockLedgerTracksTheYard(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")

	first := newVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000001")
	newVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000002")

	summary := func() store.StockSummary {
		t.Helper()
		var s store.StockSummary
		err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
			var err error
			s, err = q.StockSummaryFor(ctx, alpha.ID)
			return err
		})
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		return s
	}

	if s := summary(); s.Available != 2 || s.OnHand() != 2 || s.LedgerTotal != 2 {
		t.Fatalf("after intake: %+v", s)
	}

	// A reservation is a status change, not a ledger movement: the unit is
	// still in the yard.
	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		before, after, err := q.TransitionVehicle(ctx, alpha.ID, first.ID, store.StatusReserved)
		if err != nil {
			return err
		}
		if before.Status != store.StatusAvailable || after.Status != store.StatusReserved {
			t.Fatalf("transition = %s -> %s", before.Status, after.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if s := summary(); s.Available != 1 || s.Reserved != 1 || s.LedgerTotal != 2 || s.OnHand() != 2 {
		t.Fatalf("after reserve: %+v", s)
	}

	// Reserving twice is refused by the status guard.
	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		_, _, err := q.TransitionVehicle(ctx, alpha.ID, first.ID, store.StatusReserved)
		return err
	})
	if !errors.Is(err, store.ErrBadTransition) {
		t.Fatalf("reserve twice = %v, want ErrBadTransition", err)
	}

	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		_, _, err := q.TransitionVehicle(ctx, alpha.ID, first.ID, store.StatusAvailable)
		return err
	})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if s := summary(); s.Available != 2 || s.LedgerTotal != 2 {
		t.Fatalf("after release: %+v", s)
	}
}

// The guard is a database trigger, so a future code path that forgets the Go
// checks still cannot sell one unit twice or receive it twice.
func TestLedgerGuardRejectsImpossibleMovements(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	v := newVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000003")

	if err := insertRawMovement(alpha.ID, v.ID, "RECEIVE", 1); sqlStateOf(err) != "STK01" {
		t.Fatalf("second receive = %v, want STK01", err)
	}

	if err := insertRawMovement(alpha.ID, v.ID, "SELL", -1); err != nil {
		t.Fatalf("first sell: %v", err)
	}
	if err := insertRawMovement(alpha.ID, v.ID, "SELL", -1); sqlStateOf(err) != "STK01" {
		t.Fatalf("second sell = %v, want STK01", err)
	}
	// Buying the unit back is legitimate: it returns to one.
	if err := insertRawMovement(alpha.ID, v.ID, "RECEIVE", 1); err != nil {
		t.Fatalf("receive back: %v", err)
	}
	if err := insertRawMovement(alpha.ID, v.ID, "RECEIVE", 1); sqlStateOf(err) != "STK01" {
		t.Fatalf("receive twice = %v, want STK01", err)
	}
}

// insertRawMovement writes a ledger row without going through the store, the
// way a future feature or a manual fix might. The database must still refuse.
func insertRawMovement(tenantID, vehicleID uuid.UUID, kind string, delta int) error {
	ctx := context.Background()
	tx, err := runtime.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into stock_movements (tenant_id, vehicle_id, kind, delta, to_status)
		values ($1, $2, $3, $4, 'AVAILABLE')`, tenantID, vehicleID, kind, delta); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func parseDate(t *testing.T, value string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parse date %q: %v", value, err)
	}
	return d
}

func sqlStateOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
