package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) settingsPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		var settings store.Settings
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var innerErr error
			settings, innerErr = q.SettingsFor(r.Context(), t.ID)
			return innerErr
		})
		if err != nil {
			s.internalError(w, r, "settings", err)
			return
		}
		s.render(w, r, http.StatusOK, views.Settings(views.SettingsData{
			TenantName:    t.Name,
			UserName:      u.User.Name,
			Nav:           navLinks(r.Context(), r.URL.Path),
			CSRFToken:     csrfToken(r.Context()),
			CompanyName:   settings.CompanyName,
			Address:       settings.Address,
			Phone:         settings.Phone,
			Email:         settings.Email,
			VATNumber:     settings.VATNumber,
			DiscountLimit: strconv.Itoa(settings.DiscountLimitPercent),
			VarianceAlert: money.Plain(settings.VarianceAlertCents),
			BackdateDays:  strconv.Itoa(settings.BackdateDays),
			AlertEmail:    settings.AlertEmail,
			AlertTelegram: settings.AlertTelegramChat,
			CanWrite:      auth.Can(r.Context(), "settings.write"),
		}))
	}
}

func (s *Server) settingsSave() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		fields := settingsFields(r)
		discountLimit, err := parsePercent(fields.DiscountLimitPercent)
		if err != nil {
			s.render(w, r, http.StatusBadRequest, views.Settings(views.SettingsData{
				TenantName:    t.Name,
				UserName:      u.User.Name,
				Nav:           navLinks(r.Context(), r.URL.Path),
				CSRFToken:     csrfToken(r.Context()),
				CompanyName:   fields.CompanyName,
				Address:       fields.Address,
				Phone:         fields.Phone,
				Email:         fields.Email,
				VATNumber:     fields.VATNumber,
				DiscountLimit: fields.DiscountLimitPercent,
				VarianceAlert: fields.VarianceAlert,
				BackdateDays:  fields.BackdateDays,
				AlertEmail:    fields.AlertEmail,
				AlertTelegram: fields.AlertTelegramChat,
				CanWrite:      auth.Can(r.Context(), "settings.write"),
				Error:         "Discount percent must be 0-100.",
			}))
			return
		}
		varianceAlert, verr := money.Parse(fields.VarianceAlert)
		if verr != nil {
			varianceAlert = 0
		}
		backdateDays, err := strconv.Atoi(strings.TrimSpace(fields.BackdateDays))
		if err != nil || backdateDays < 0 || backdateDays > 90 {
			backdateDays = 0
		}
		var savedErr error
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var innerErr error
			_, innerErr = q.SaveSettings(r.Context(), t.ID, u.User.ID, store.SettingsParams{
				CompanyName:          fields.CompanyName,
				Address:              fields.Address,
				Phone:                fields.Phone,
				Email:                fields.Email,
				VATNumber:            fields.VATNumber,
				DiscountLimitPercent: discountLimit,
				VarianceAlertCents:   varianceAlert,
				BackdateDays:         backdateDays,
				AlertEmail:           fields.AlertEmail,
				AlertTelegramChat:    fields.AlertTelegramChat,
			})
			savedErr = innerErr
			return innerErr
		})
		if err != nil || savedErr != nil {
			if errors.Is(savedErr, store.ErrInvalidSettings) {
				s.render(w, r, http.StatusBadRequest, views.Settings(views.SettingsData{
					TenantName: t.Name,
					UserName:   u.User.Name,
					Nav:        navLinks(r.Context(), r.URL.Path),
					CSRFToken:  csrfToken(r.Context()),
					CanWrite:   auth.Can(r.Context(), "settings.write"),
					Error:      "Check the discount percent (0-100), backdate days (0-90) and variance alert.",
				}))
				return
			}
			s.internalError(w, r, "settings save", savedErr)
			return
		}
		http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
	}
}

func settingsFields(r *http.Request) struct {
	CompanyName          string
	Address              string
	Phone                string
	Email                string
	VATNumber            string
	DiscountLimitPercent string
	VarianceAlert        string
	BackdateDays         string
	AlertEmail           string
	AlertTelegramChat    string
} {
	return struct {
		CompanyName          string
		Address              string
		Phone                string
		Email                string
		VATNumber            string
		DiscountLimitPercent string
		VarianceAlert        string
		BackdateDays         string
		AlertEmail           string
		AlertTelegramChat    string
	}{
		CompanyName:          strings.TrimSpace(r.PostFormValue("company_name")),
		Address:              strings.TrimSpace(r.PostFormValue("address")),
		Phone:                strings.TrimSpace(r.PostFormValue("phone")),
		Email:                strings.ToLower(strings.TrimSpace(r.PostFormValue("email"))),
		VATNumber:            strings.TrimSpace(r.PostFormValue("vat_number")),
		DiscountLimitPercent: strings.TrimSpace(r.PostFormValue("discount_limit_percent")),
		VarianceAlert:        strings.TrimSpace(r.PostFormValue("variance_alert_cents")),
		BackdateDays:         strings.TrimSpace(r.PostFormValue("backdate_days")),
		AlertEmail:           strings.ToLower(strings.TrimSpace(r.PostFormValue("alert_email"))),
		AlertTelegramChat:    strings.TrimSpace(r.PostFormValue("alert_telegram_chat")),
	}
}

func parsePercent(raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if n < 0 || n > 100 {
		return 0, errors.New("percent must be between 0 and 100")
	}
	return n, nil
}
