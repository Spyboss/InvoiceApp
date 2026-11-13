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

func TestSettingsRefusesBadValues(t *testing.T) {
	st := freshStore(t)
	alpha := newTenant(t, st, "Beta", "beta.local")
	owner := newUser(t, st, alpha.ID, "o@beta.local", store.RoleOwner)

	bad := []store.SettingsParams{
		{DiscountLimitPercent: 101},
		{BackdateDays: 91},
		{VarianceAlertCents: -1},
		{SeriesPrefix: "TM1"},
		{SeriesPrefix: "TM01X"},
		{SeriesPrefix: "TM-1"},
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

// TestSeriesPrefixIsTheOwnersToChoose is D27: the four characters on the paper
// come from the settings screen, and leaving the box empty hands them back to
// the slug rather than printing nothing.
func TestSeriesPrefixIsTheOwnersToChoose(t *testing.T) {
	st := freshStore(t)
	alpha := newSluggedTenant(t, st, "alpha-motors", "Alpha Motors", "alpha.local")
	owner := newUser(t, st, alpha.ID, "owner@alpha.local", store.RoleOwner)
	ctx := context.Background()

	save := func(prefix string) {
		t.Helper()
		err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
			_, err := q.SaveSettings(ctx, alpha.ID, owner.ID, store.SettingsParams{SeriesPrefix: prefix})
			return err
		})
		if err != nil {
			t.Fatalf("save prefix %q: %v", prefix, err)
		}
	}

	var stored store.Settings
	var effective string
	read := func() {
		t.Helper()
		err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
			var innerErr error
			if stored, innerErr = q.SettingsFor(ctx, alpha.ID); innerErr != nil {
				return innerErr
			}
			effective, innerErr = q.EffectiveSeriesPrefix(ctx, alpha.ID)
			return innerErr
		})
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
	}

	// Nothing saved: the slug supplies the code.
	read()
	if effective != "ALPH" {
		t.Fatalf("effective prefix before saving = %q, want the slug's ALPH", effective)
	}

	// What the owner types is stored upper cased, and it is what prints.
	save("tm01")
	read()
	if stored.SeriesPrefix != "TM01" {
		t.Errorf("stored prefix = %q, want it upper cased", stored.SeriesPrefix)
	}
	if effective != "TM01" {
		t.Errorf("effective prefix = %q, want the stored one", effective)
	}

	// Blank is not "keep the old one": it is "work it out again".
	save("")
	read()
	if stored.SeriesPrefix != "" {
		t.Errorf("stored prefix after clearing = %q, want blank", stored.SeriesPrefix)
	}
	if effective != "ALPH" {
		t.Errorf("effective prefix after clearing = %q, want the slug's ALPH", effective)
	}
}
