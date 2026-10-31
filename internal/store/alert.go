package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Alert kinds. These are the codes stored in the database and the strings the
// sender matches on; a translated sentence never reaches the row.
const (
	AlertVoid         = "VOID"
	AlertVariance     = "VARIANCE"
	AlertNumberingGap = "NUMBERING_GAP"
	AlertOverride     = "OVERRIDE"
	AlertAfterHours   = "AFTER_HOURS"
)

// Alert severities. The sender decides what to do with each: an info alert is
// an email, a critical one is also a telegram message.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Alert statuses.
const (
	AlertPending = "pending"
	AlertSent    = "sent"
	AlertFailed  = "failed"
)

// Alert is one fact that needed a human: a void, a variance past the
// threshold, a numbering gap. It is written in the same transaction as the
// event, so a crash between "this happened" and "we told someone" cannot lose
// it. The sender marks it sent or failed after the fact.
type Alert struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Kind      string
	Message   string
	Severity  string
	Entity    string
	Status    string
	Attempts  int
	LastError string
	CreatedAt time.Time
	SentAt    *time.Time
}

// AlertParams is what a caller records. The message is the sentence the owner
// reads; it is written in the caller's own words because a void and a variance
// explain themselves differently.
type AlertParams struct {
	Kind     string
	Message  string
	Severity string
	Entity   string
}

// ValidAlertKind reports whether a kind is one this app sends.
func ValidAlertKind(kind string) bool {
	switch kind {
	case AlertVoid, AlertVariance, AlertNumberingGap, AlertOverride, AlertAfterHours:
		return true
	}
	return false
}

// RecordAlert writes one alert inside the caller's transaction. It refuses a
// blank or over-long message, and an unknown kind, so a typo cannot create a
// row the sender will never understand. Must run inside InTenant.
func (s *Store) RecordAlert(ctx context.Context, tenantID uuid.UUID, p AlertParams) (Alert, error) {
	message := strings.TrimSpace(p.Message)
	if !ValidAlertKind(p.Kind) || utf8.RuneCountInString(message) < 1 || utf8.RuneCountInString(message) > 1000 {
		return Alert{}, ErrInvalidAlert
	}
	severity := p.Severity
	switch severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		severity = SeverityInfo
	}
	if utf8.RuneCountInString(p.Entity) > 200 {
		return Alert{}, ErrInvalidAlert
	}

	const q = `
		insert into alerts (tenant_id, kind, message, severity, entity)
		values ($1, $2, $3, $4, $5)
		returning id, tenant_id, kind, message, severity, entity, status, attempts,
			last_error, created_at, sent_at`
	a, err := scanAlert(s.db.QueryRow(ctx, q, tenantID, p.Kind, message, severity, p.Entity))
	if err != nil {
		return Alert{}, fmt.Errorf("record alert: %w", err)
	}
	return a, nil
}

// PendingAlerts returns the alerts a sender has not yet delivered, oldest
// first, so the sender works through the queue in the order things happened.
// Must run inside InTenant.
func (s *Store) PendingAlerts(ctx context.Context, tenantID uuid.UUID, limit int) ([]Alert, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		select id, tenant_id, kind, message, severity, entity, status, attempts,
			last_error, created_at, sent_at
		from alerts
		where tenant_id = $1 and status = $2
		order by created_at
		limit $3`
	rows, err := s.db.Query(ctx, q, tenantID, AlertPending, limit)
	if err != nil {
		return nil, fmt.Errorf("pending alerts: %w", err)
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending alerts: %w", err)
	}
	return out, nil
}

// MarkAlertSent records that the sender delivered an alert. It is the one
// write the runtime role may make to an alert, and it only touches the columns
// the sender owns, so an alert's message and kind can never be rewritten.
// Must run inside InTenant.
func (s *Store) MarkAlertSent(ctx context.Context, tenantID, alertID uuid.UUID) error {
	const q = `
		update alerts
		set status = $3, sent_at = now(), attempts = attempts + 1, last_error = ''
		where tenant_id = $1 and id = $2`
	if _, err := s.db.Exec(ctx, q, tenantID, alertID, AlertSent); err != nil {
		return fmt.Errorf("mark alert sent: %w", err)
	}
	return nil
}

// maxAlertAttempts is how many times the sender tries one alert before it
// gives up and leaves it failed on the record. Three tries spread over three
// sender cycles is enough to ride out a mailbox that is briefly down without
// queueing the same message forever.
const maxAlertAttempts = 3

// MarkAlertFailed records why an alert could not be sent. The row stays in the
// queue for a later retry until it has been tried maxAlertAttempts times; only
// then does it go terminal, and the error is kept so the owner can see what
// went wrong. Must run inside InTenant.
func (s *Store) MarkAlertFailed(ctx context.Context, tenantID, alertID uuid.UUID, sendErr string) error {
	if utf8.RuneCountInString(sendErr) > 500 {
		sendErr = sendErr[:500]
	}
	const q = `
		update alerts
		set status = case when attempts + 1 >= $3 then $4 else $5 end,
			attempts = attempts + 1,
			last_error = $6
		where tenant_id = $1 and id = $2`
	if _, err := s.db.Exec(ctx, q, tenantID, alertID, maxAlertAttempts, AlertFailed, AlertPending, sendErr); err != nil {
		return fmt.Errorf("mark alert failed: %w", err)
	}
	return nil
}

// ListAlerts returns the most recent alerts for a tenant, newest first, so the
// owner can see what has fired. Must run inside InTenant.
func (s *Store) ListAlerts(ctx context.Context, tenantID uuid.UUID, limit int) ([]Alert, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		select id, tenant_id, kind, message, severity, entity, status, attempts,
			last_error, created_at, sent_at
		from alerts
		where tenant_id = $1
		order by created_at desc
		limit $2`
	rows, err := s.db.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	return out, nil
}

func scanAlert(row rowScanner) (Alert, error) {
	var a Alert
	err := row.Scan(&a.ID, &a.TenantID, &a.Kind, &a.Message, &a.Severity, &a.Entity,
		&a.Status, &a.Attempts, &a.LastError, &a.CreatedAt, &a.SentAt)
	if err != nil {
		return Alert{}, fmt.Errorf("scan alert: %w", notFound(err))
	}
	return a, nil
}
