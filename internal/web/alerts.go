package web

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// alertsPage shows what has fired. It also gives the sender another try at
// anything still pending: a person opening the page is a natural moment to
// push again, and it means a mailbox that was down yesterday heals without a
// scheduler.
func (s *Server) alertsPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		if s.alerts != nil {
			if _, err := s.alerts.DeliverPending(r.Context(), t.ID); err != nil {
				// A delivery failure is not a reason to hide the page: the
				// alerts themselves are the point, and each row carries its
				// own delivery state.
				s.log.Warn("alert delivery failed", "error", err, "tenant", t.ID)
			}
		}

		var (
			rows     []store.Alert
			settings store.Settings
		)
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			rows, err = q.ListAlerts(r.Context(), t.ID, 200)
			if err != nil {
				return err
			}
			settings, err = q.SettingsFor(r.Context(), t.ID)
			return err
		})
		if err != nil {
			s.internalError(w, r, "alerts", err)
			return
		}

		s.render(w, r, http.StatusOK, views.Alerts(views.AlertsData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			Rows:       alertRows(rows),
			Email:      settings.AlertEmail,
			Telegram:   settings.AlertTelegramChat,
		}))
	}
}

func alertRows(alerts []store.Alert) []views.AlertRow {
	rows := make([]views.AlertRow, 0, len(alerts))
	for _, a := range alerts {
		sent := ""
		if a.SentAt != nil {
			sent = a.SentAt.UTC().Format("2006-01-02 15:04")
		}
		rows = append(rows, views.AlertRow{
			Kind:      a.Kind,
			KindLabel: alertKindLabel(a.Kind),
			Message:   a.Message,
			Severity:  a.Severity,
			Status:    a.Status,
			Attempts:  a.Attempts,
			LastError: a.LastError,
			At:        a.CreatedAt,
			Sent:      sent,
		})
	}
	return rows
}

// alertKindLabel spells the stored code out. The code stays in the row and in
// the sender's matcher; the owner reads the sentence.
func alertKindLabel(kind string) string {
	switch kind {
	case store.AlertVoid:
		return "Void"
	case store.AlertVariance:
		return "Drawer variance"
	case store.AlertNumberingGap:
		return "Missing number"
	case store.AlertOverride:
		return "Override"
	case store.AlertAfterHours:
		return "After hours"
	}
	return kind
}

// deliverAlerts pushes whatever a just-committed transaction queued. It runs
// on its own short context so a slow relay cannot hold the response open, and
// it is deliberately fire and forget: the queue is the record, the send is
// best effort, and a failure is picked up the next time somebody opens the
// alerts page.
func (s *Server) deliverAlerts(tenantID uuid.UUID) {
	if s.alerts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := s.alerts.DeliverPending(ctx, tenantID); err != nil {
		s.log.Warn("alert delivery failed", "error", err)
	}
}
