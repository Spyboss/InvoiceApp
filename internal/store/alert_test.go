package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// recordAlert writes one alert inside the tenant's transaction, the way a
// caller records it next to the event that caused it.
func recordAlert(t *testing.T, st *store.Store, tenantID uuid.UUID, p store.AlertParams) (store.Alert, error) {
	t.Helper()
	var a store.Alert
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		a, err = q.RecordAlert(context.Background(), tenantID, p)
		return err
	})
	return a, err
}

func TestRecordAlertWritesAFact(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")

	a, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:     store.AlertVoid,
		Message:  "Invoice INV-0007 was voided by the owner",
		Severity: store.SeverityWarning,
		Entity:   "INV-0007",
	})
	if err != nil {
		t.Fatalf("record alert: %v", err)
	}
	if a.Status != store.AlertPending {
		t.Errorf("status = %q, want pending", a.Status)
	}
	if a.Attempts != 0 || a.LastError != "" {
		t.Errorf("fresh alert has attempts=%d last_error=%q, want 0 and empty", a.Attempts, a.LastError)
	}
	if a.SentAt != nil {
		t.Errorf("sent_at = %v, want nil before sending", a.SentAt)
	}
	if a.Kind != store.AlertVoid || a.Severity != store.SeverityWarning || a.Entity != "INV-0007" {
		t.Errorf("alert = %+v, want the facts that were recorded", a)
	}
}

func TestRecordAlertRefusesBadInput(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")

	if _, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    "NOT_A_KIND",
		Message: "something happened",
	}); !errors.Is(err, store.ErrInvalidAlert) {
		t.Errorf("unknown kind = %v, want ErrInvalidAlert", err)
	}

	if _, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    store.AlertVoid,
		Message: "   ",
	}); !errors.Is(err, store.ErrInvalidAlert) {
		t.Errorf("blank message = %v, want ErrInvalidAlert", err)
	}

	if _, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    store.AlertVoid,
		Message: string(make([]byte, 1001)),
	}); !errors.Is(err, store.ErrInvalidAlert) {
		t.Errorf("over-long message = %v, want ErrInvalidAlert", err)
	}
}

func TestMarkAlertSentAndFailed(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")

	a, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    store.AlertVariance,
		Message: "Drawer is short by 1,200.00",
	})
	if err != nil {
		t.Fatalf("record alert: %v", err)
	}

	// The sender delivered it.
	err = st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		return q.MarkAlertSent(context.Background(), alpha.ID, a.ID)
	})
	if err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	sent, err := firstAlert(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if sent.Status != store.AlertSent || sent.SentAt == nil || sent.Attempts != 1 {
		t.Errorf("sent alert = %+v, want status sent, sent_at set, attempts 1", sent)
	}
	if sent.LastError != "" {
		t.Errorf("last_error = %q, want empty after a clean send", sent.LastError)
	}

	// A send that fails keeps the row in the queue for a retry, with the
	// error kept so the owner can see it.
	b, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    store.AlertNumberingGap,
		Message: "Number 0007 was never issued in the sales series",
	})
	if err != nil {
		t.Fatalf("record alert: %v", err)
	}
	err = st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		return q.MarkAlertFailed(context.Background(), alpha.ID, b.ID, "smtp: connection refused")
	})
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	pending, err := pendingAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != b.ID {
		t.Fatalf("pending after one failure = %+v, want the failed alert still queued", pending)
	}
	if pending[0].Attempts != 1 || pending[0].LastError != "smtp: connection refused" {
		t.Errorf("failed alert = %+v, want attempts 1 and the error kept", pending[0])
	}

	// After the retry budget it goes terminal and leaves the queue.
	for i := 0; i < 2; i++ {
		err = st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
			return q.MarkAlertFailed(context.Background(), alpha.ID, b.ID, "smtp: connection refused")
		})
		if err != nil {
			t.Fatalf("mark failed retry %d: %v", i, err)
		}
	}
	pending, err = pendingAlerts(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after the retry budget = %+v, want none", pending)
	}
	terminal, err := firstAlert(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if terminal.Status != store.AlertFailed || terminal.Attempts != 3 {
		t.Errorf("terminal alert = %+v, want status failed and 3 attempts", terminal)
	}
}

func TestAlertsAreInvisibleAcrossTenants(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")

	if _, err := recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    store.AlertVoid,
		Message: "Alpha's void",
	}); err != nil {
		t.Fatalf("record alert: %v", err)
	}

	// Beta's tenant sees nothing of Alpha's alert, not by listing and not by
	// trying to mark it sent.
	list, err := listAlerts(t, st, beta.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("beta sees %d alerts, want 0", len(list))
	}
	pending, err := pendingAlerts(t, st, beta.ID)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("beta's pending queue = %+v, want empty", pending)
	}

	// And the store refuses to touch an alert that belongs to another tenant.
	var alphaAlert store.Alert
	alphaAlert, err = recordAlert(t, st, alpha.ID, store.AlertParams{
		Kind:    store.AlertVariance,
		Message: "Alpha's variance",
	})
	if err != nil {
		t.Fatalf("record alert: %v", err)
	}
	err = st.InTenant(context.Background(), beta.ID, func(q *store.Store) error {
		return q.MarkAlertSent(context.Background(), beta.ID, alphaAlert.ID)
	})
	if err != nil {
		t.Fatalf("cross-tenant mark sent: %v", err)
	}
	seen, err := firstAlert(t, st, alpha.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if seen.Status != store.AlertPending {
		t.Errorf("alpha's alert status = %q, want still pending after beta's touch", seen.Status)
	}
}

// firstAlert reads the newest alert for a tenant, so a test can assert on what
// was just written.
func firstAlert(t *testing.T, st *store.Store, tenantID uuid.UUID) (store.Alert, error) {
	t.Helper()
	list, err := listAlerts(t, st, tenantID)
	if err != nil {
		return store.Alert{}, err
	}
	if len(list) == 0 {
		t.Fatal("no alerts for the tenant")
	}
	return list[0], nil
}

func listAlerts(t *testing.T, st *store.Store, tenantID uuid.UUID) ([]store.Alert, error) {
	t.Helper()
	var out []store.Alert
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		out, err = q.ListAlerts(context.Background(), tenantID, 100)
		return err
	})
	return out, err
}

func pendingAlerts(t *testing.T, st *store.Store, tenantID uuid.UUID) ([]store.Alert, error) {
	t.Helper()
	var out []store.Alert
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		out, err = q.PendingAlerts(context.Background(), tenantID, 100)
		return err
	})
	return out, err
}
