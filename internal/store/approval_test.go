package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// today is the business date these tests pretend it is. The backdate rule
// compares a document against it, so it is fixed rather than read from the
// clock, and a test that runs at midnight never flakes.
var today = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)

// approvalScenario builds a tenant with an owner and a manager, a customer and
// a unit priced at 1,250,000. It returns the pieces the approval tests need.
type approvalScenario struct {
	st       *store.Store
	tenant   store.Tenant
	owner    store.User
	manager  store.User
	customer store.Customer
	vehicle  store.Vehicle
}

func newApprovalScenario(t *testing.T) approvalScenario {
	t.Helper()
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, alpha.ID, "owner@alpha.test", store.RoleOwner)
	manager := newUser(t, st, alpha.ID, "manager@alpha.test", store.RoleManager)
	customer, err := newCustomer(t, st, alpha.ID, owner.ID, "Saman Perera", "907654321V")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	vehicle := sellableVehicle(t, st, alpha.ID, owner.ID, "ZAPM0000000200", 125000000)
	return approvalScenario{
		st:       st,
		tenant:   alpha,
		owner:    owner,
		manager:  manager,
		customer: customer,
		vehicle:  vehicle,
	}
}

// saveSettings writes the tenant's approval numbers so the tests below can
// reason about them.
func saveSettings(t *testing.T, st *store.Store, tenantID, actorID uuid.UUID, p store.SettingsParams) {
	t.Helper()
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		_, err := q.SaveSettings(context.Background(), tenantID, actorID, p)
		return err
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
}

func TestDiscountAboveLimitNeedsApproval(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st

	// The owner sets a 5 percent approval limit.
	saveSettings(t, st, s.tenant.ID, s.owner.ID, store.SettingsParams{
		CompanyName:          "Alpha Motors",
		DiscountLimitPercent: 5,
	})

	// A price 10 percent below the list is past the limit, so the sale is
	// refused until a manager pins it.
	_, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 112500000, // 10 percent below 1,250,000
	})
	if !errors.Is(err, store.ErrNeedsApproval) {
		t.Fatalf("discount past the limit = %v, want ErrNeedsApproval", err)
	}

	// A manager pins it and the sale goes through, with both people on the
	// document.
	res, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:      store.DocSalesCash,
		DocDate:      today,
		Today:        today,
		CustomerID:   s.customer.ID,
		VehicleID:    s.vehicle.ID,
		PriceCents:   112500000,
		ApprovedBy:   &s.manager.ID,
		ApprovalNote: "discount cleared with the owner",
	})
	if err != nil {
		t.Fatalf("approved sale: %v", err)
	}
	if res.Document.ApprovedBy == nil || *res.Document.ApprovedBy != s.manager.ID {
		t.Fatalf("approved_by = %v, want %v", res.Document.ApprovedBy, s.manager.ID)
	}
	if res.Document.ApprovalNote != "discount cleared with the owner" {
		t.Errorf("approval note = %q", res.Document.ApprovalNote)
	}
	if res.Document.CreatedBy == nil || *res.Document.CreatedBy != s.owner.ID {
		t.Errorf("created_by = %v, want the clerk", res.Document.CreatedBy)
	}
}

func TestDiscountWithinLimitNeedsNoApproval(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st
	saveSettings(t, st, s.tenant.ID, s.owner.ID, store.SettingsParams{
		CompanyName:          "Alpha Motors",
		DiscountLimitPercent: 20,
	})

	// 10 percent off a 20 percent limit is ordinary counter work.
	res, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 112500000,
	})
	if err != nil {
		t.Fatalf("ordinary sale: %v", err)
	}
	if res.Document.ApprovedBy != nil {
		t.Errorf("approved_by = %v, want nil", res.Document.ApprovedBy)
	}
}

func TestBackdateBeyondLimitNeedsApproval(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st
	saveSettings(t, st, s.tenant.ID, s.owner.ID, store.SettingsParams{
		CompanyName:  "Alpha Motors",
		BackdateDays: 1,
	})

	// A document dated a week ago is past the one day limit.
	_, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-23"),
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 125000000,
	})
	if !errors.Is(err, store.ErrNeedsApproval) {
		t.Fatalf("backdate past the limit = %v, want ErrNeedsApproval", err)
	}

	// Yesterday is inside a one day limit, so it needs no pin.
	_, err = createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    parseDate(t, "2026-10-29"),
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("yesterday inside the limit: %v", err)
	}
}

func TestSalesCannotPinAnApproval(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st
	saveSettings(t, st, s.tenant.ID, s.owner.ID, store.SettingsParams{
		CompanyName:          "Alpha Motors",
		DiscountLimitPercent: 5,
	})

	sales := newUser(t, st, s.tenant.ID, "sales@alpha.test", store.RoleSales)
	_, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 112500000,
		ApprovedBy: &sales.ID,
	})
	if !errors.Is(err, store.ErrNeedsApproval) {
		t.Fatalf("sales pin = %v, want ErrNeedsApproval", err)
	}
}

func TestVoidKeepsThePaperAndReturnsTheUnit(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st

	res, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("sale: %v", err)
	}
	if res.VehicleAfter != store.StatusSold {
		t.Fatalf("vehicle after sale = %q, want SOLD", res.VehicleAfter)
	}

	// The manager voids it with a reason.
	var voided store.Document
	err = st.InTenant(context.Background(), s.tenant.ID, func(q *store.Store) error {
		var innerErr error
		voided, innerErr = q.VoidDocument(context.Background(), s.tenant.ID, res.Document.ID, s.manager.ID, "wrong customer on the paper", today)
		return innerErr
	})
	if err != nil {
		t.Fatalf("void: %v", err)
	}
	if voided.Status != store.DocVoid {
		t.Fatalf("status = %q, want VOID", voided.Status)
	}
	if voided.VoidedBy == nil || *voided.VoidedBy != s.manager.ID {
		t.Errorf("voided_by = %v, want the manager", voided.VoidedBy)
	}
	if voided.VoidReason != "wrong customer on the paper" {
		t.Errorf("void reason = %q", voided.VoidReason)
	}
	// Every printed field survives: the paper is still the paper.
	if voided.PriceCents != res.Document.PriceCents || voided.DocNo != res.Document.DocNo {
		t.Errorf("printed fields changed: %+v", voided)
	}

	// The unit is back in the yard and the ledger totals to zero.
	vehicle, err := vehicleByID(t, st, s.tenant.ID, s.vehicle.ID)
	if err != nil {
		t.Fatalf("vehicle: %v", err)
	}
	if vehicle.Status != store.StatusAvailable {
		t.Errorf("vehicle after void = %q, want AVAILABLE", vehicle.Status)
	}
	total, err := ledgerTotal(t, s.tenant.ID, s.vehicle.ID)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if total != 0 {
		t.Errorf("ledger total = %d, want 0", total)
	}
}

func TestVoidWithoutReasonIsRefused(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st

	res, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("sale: %v", err)
	}

	err = st.InTenant(context.Background(), s.tenant.ID, func(q *store.Store) error {
		_, innerErr := q.VoidDocument(context.Background(), s.tenant.ID, res.Document.ID, s.manager.ID, "", today)
		return innerErr
	})
	if !errors.Is(err, store.ErrInvalidDocument) {
		t.Fatalf("void without reason = %v, want ErrInvalidDocument", err)
	}
}

func TestVoidOnADayAlreadyClosedIsRefused(t *testing.T) {
	s := newApprovalScenario(t)
	st := s.st

	res, err := createDocument(t, st, s.tenant.ID, s.owner.ID, store.DocumentParams{
		DocType:    store.DocSalesCash,
		DocDate:    today,
		Today:      today,
		CustomerID: s.customer.ID,
		VehicleID:  s.vehicle.ID,
		PriceCents: 125000000,
	})
	if err != nil {
		t.Fatalf("sale: %v", err)
	}

	// Close the day, then try to void on it.
	err = st.InTenant(context.Background(), s.tenant.ID, func(q *store.Store) error {
		_, innerErr := q.CloseDay(context.Background(), s.tenant.ID, s.owner.ID, store.DayEndParams{
			Window: store.DayWindow{
				Date:  today,
				Start: today,
				End:   today.AddDate(0, 0, 1),
			},
			OpeningFloatCents: 0,
			CountedCents:      0,
		})
		return innerErr
	})
	if err != nil {
		t.Fatalf("close day: %v", err)
	}

	err = st.InTenant(context.Background(), s.tenant.ID, func(q *store.Store) error {
		_, innerErr := q.VoidDocument(context.Background(), s.tenant.ID, res.Document.ID, s.manager.ID, "after the z", today)
		return innerErr
	})
	if !errors.Is(err, store.ErrDayAlreadyClosed) {
		t.Fatalf("void on a closed day = %v, want ErrDayAlreadyClosed", err)
	}
}

// vehicleByID reads one unit back so a test can assert on its status.
func vehicleByID(t *testing.T, st *store.Store, tenantID, id uuid.UUID) (store.Vehicle, error) {
	t.Helper()
	var v store.Vehicle
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var innerErr error
		v, innerErr = q.VehicleByID(context.Background(), tenantID, id)
		return innerErr
	})
	return v, err
}

// ledgerTotal sums a unit's stock movements. The guard keeps it in {0,1}, so a
// sale is 0 and a returned unit is back to 0.
func ledgerTotal(t *testing.T, tenantID, vehicleID uuid.UUID) (int, error) {
	t.Helper()
	if runtime == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	var total int
	err := runtime.QueryRow(context.Background(),
		`select coalesce(sum(delta), 0) from stock_movements
		 where tenant_id = $1 and vehicle_id = $2`,
		tenantID, vehicleID).Scan(&total)
	return total, err
}
