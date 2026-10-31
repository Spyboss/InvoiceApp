package store_test

import (
	"context"
	"testing"

	"github.com/spyboss/invoiceapp/internal/store"
)

func TestSettingsDefaultAndSave(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Alpha Motors", "alpha.local")
	owner := newUser(t, st, alpha.ID, "owner@alpha.local", store.RoleOwner)

	err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		_, err := q.SaveSettings(context.Background(), alpha.ID, owner.ID, store.SettingsParams{
			CompanyName:          "Alpha Motors",
			DiscountLimitPercent: 10,
			VarianceAlertCents:   500000,
			BackdateDays:         1,
			AlertEmail:           "owner@alpha.local",
		})
		return err
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	var s store.Settings
	err = st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
		var err error
		s, err = q.SettingsFor(context.Background(), alpha.ID)
		return err
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if s.CompanyName != "Alpha Motors" || s.DiscountLimitPercent != 10 || s.VarianceAlertCents != 500000 {
		t.Fatalf("settings = %+v", s)
	}
	if s.TenantID != alpha.ID {
		t.Fatalf("tenant = %v", s.TenantID)
	}
}

func TestSettingsRefusesBadDiscountOrBackdate(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Beta", "beta.local")
	owner := newUser(t, st, alpha.ID, "o@beta.local", store.RoleOwner)

	bad := []store.SettingsParams{
		{DiscountLimitPercent: 101},
		{BackdateDays: 91},
		{VarianceAlertCents: -1},
	}
	for _, p := range bad {
		err := st.InTenant(context.Background(), alpha.ID, func(q *store.Store) error {
			_, err := q.SaveSettings(context.Background(), alpha.ID, owner.ID, p)
			return err
		})
		if err == nil {
			t.Fatalf("expected refusal for %+v", p)
		}
	}
}