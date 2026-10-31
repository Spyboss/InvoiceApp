package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Settings is how one dealership runs its paperwork: what its paper says, how
// far a clerk may bend a price, and where an alert goes. The zero value is the
// default, so a tenant that has never opened the screen reads sensible
// numbers rather than an error: no discount without approval, today only, any
// drawer difference alerted.
type Settings struct {
	TenantID uuid.UUID

	CompanyName string
	Address     string
	Phone       string
	Email       string
	VATNumber   string

	DiscountLimitPercent int
	VarianceAlertCents   int64
	BackdateDays         int

	AlertEmail        string
	AlertTelegramChat string

	UpdatedBy *uuid.UUID
	UpdatedAt *time.Time
}

// SettingsParams is what the owner typed. Every field is stored as given, so a
// blank means "not set", never "leave as it was".
type SettingsParams struct {
	CompanyName string
	Address     string
	Phone       string
	Email       string
	VATNumber   string

	DiscountLimitPercent int
	VarianceAlertCents   int64
	BackdateDays         int

	AlertEmail        string
	AlertTelegramChat string
}

const settingsColumns = `tenant_id, company_name, address, phone, email, vat_number,
	discount_limit_percent, variance_alert_cents, backdate_days,
	alert_email, alert_telegram_chat, updated_by, updated_at`

// SettingsFor reads the tenant's settings, or the defaults when the screen has
// never been saved. Must run inside InTenant.
func (s *Store) SettingsFor(ctx context.Context, tenantID uuid.UUID) (Settings, error) {
	const q = `select ` + settingsColumns + ` from tenant_settings where tenant_id = $1`

	var v Settings
	err := s.db.QueryRow(ctx, q, tenantID).Scan(&v.TenantID, &v.CompanyName, &v.Address, &v.Phone,
		&v.Email, &v.VATNumber, &v.DiscountLimitPercent, &v.VarianceAlertCents, &v.BackdateDays,
		&v.AlertEmail, &v.AlertTelegramChat, &v.UpdatedBy, &v.UpdatedAt)
	if err != nil {
		if notFound(err) == ErrNotFound {
			return Settings{TenantID: tenantID}, nil
		}
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return v, nil
}

// SaveSettings writes the tenant's settings. The ranges are checked here so an
// import script gets the same answer the screen does, and the table checks
// them again whatever writes the row. Must run inside InTenant.
func (s *Store) SaveSettings(ctx context.Context, tenantID uuid.UUID, updatedBy uuid.UUID, p SettingsParams) (Settings, error) {
	p.CompanyName = strings.TrimSpace(p.CompanyName)
	p.Address = strings.TrimSpace(p.Address)
	p.Phone = strings.TrimSpace(p.Phone)
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	p.VATNumber = strings.TrimSpace(p.VATNumber)
	p.AlertEmail = strings.ToLower(strings.TrimSpace(p.AlertEmail))
	p.AlertTelegramChat = strings.TrimSpace(p.AlertTelegramChat)

	switch {
	case utf8.RuneCountInString(p.CompanyName) > 120,
		utf8.RuneCountInString(p.Address) > 500,
		utf8.RuneCountInString(p.Phone) > 60,
		utf8.RuneCountInString(p.Email) > 200,
		utf8.RuneCountInString(p.VATNumber) > 40,
		utf8.RuneCountInString(p.AlertEmail) > 500,
		utf8.RuneCountInString(p.AlertTelegramChat) > 100:
		return Settings{}, ErrInvalidSettings
	case p.DiscountLimitPercent < 0 || p.DiscountLimitPercent > 100:
		return Settings{}, ErrInvalidSettings
	case p.BackdateDays < 0 || p.BackdateDays > 90:
		return Settings{}, ErrInvalidSettings
	case p.VarianceAlertCents < 0:
		return Settings{}, ErrInvalidSettings
	}

	const q = `
		insert into tenant_settings (tenant_id, company_name, address, phone, email, vat_number,
			discount_limit_percent, variance_alert_cents, backdate_days, alert_email,
			alert_telegram_chat, updated_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (tenant_id) do update set
			company_name = excluded.company_name,
			address = excluded.address,
			phone = excluded.phone,
			email = excluded.email,
			vat_number = excluded.vat_number,
			discount_limit_percent = excluded.discount_limit_percent,
			variance_alert_cents = excluded.variance_alert_cents,
			backdate_days = excluded.backdate_days,
			alert_email = excluded.alert_email,
			alert_telegram_chat = excluded.alert_telegram_chat,
			updated_by = excluded.updated_by,
			updated_at = now()
		returning ` + settingsColumns

	var v Settings
	if err := s.db.QueryRow(ctx, q, tenantID, p.CompanyName, p.Address, p.Phone, p.Email, p.VATNumber,
		p.DiscountLimitPercent, p.VarianceAlertCents, p.BackdateDays, p.AlertEmail,
		p.AlertTelegramChat, updatedBy).
		Scan(&v.TenantID, &v.CompanyName, &v.Address, &v.Phone, &v.Email, &v.VATNumber,
			&v.DiscountLimitPercent, &v.VarianceAlertCents, &v.BackdateDays, &v.AlertEmail,
			&v.AlertTelegramChat, &v.UpdatedBy, &v.UpdatedAt); err != nil {
		return Settings{}, fmt.Errorf("save settings: %w", err)
	}
	return v, nil
}
