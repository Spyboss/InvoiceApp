// Package notify pushes an alert to a human outside the app: an email and a
// telegram message. Both channels are optional and switch themselves off when
// unconfigured, so a dealership that has set nothing still runs and its alerts
// stay on the /alerts page.
package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// Mailer sends one email. It is an interface so the tests can prove the sender
// marks an alert sent or failed without a mailbox.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Notifier sends one chat message. Same reason it is an interface.
type Notifier interface {
	Send(ctx context.Context, chatID, text string) error
}

// alertStore is the slice of the store the sender actually uses: reading a
// tenant's settings and its queue, then recording the outcome of a send. It is
// an interface so the tests can drive the channel logic without a database.
type alertStore interface {
	SettingsFor(ctx context.Context, tenantID uuid.UUID) (store.Settings, error)
	PendingAlerts(ctx context.Context, tenantID uuid.UUID, limit int) ([]store.Alert, error)
	MarkAlertSent(ctx context.Context, tenantID, alertID uuid.UUID) error
	MarkAlertFailed(ctx context.Context, tenantID, alertID uuid.UUID, sendErr string) error
}

// Sender delivers a tenant's pending alerts. A nil Mailer or Notifier, or an
// empty address on the tenant, means that channel does not fire: the caller
// builds only the channels the deployment actually has.
type Sender struct {
	Store    alertStore
	Mail     Mailer
	Notifier Notifier
	// Now is the clock, so tests can control it. Defaults to time.Now.
	Now func() time.Time
}

// DeliverPending works through one tenant's queue, oldest first. It is called
// by the request that recorded an alert (once its transaction has committed)
// and again whenever somebody opens the alerts page, which is how a send that
// failed yesterday gets another try without a scheduler.
//
// The rule for each alert is: if the tenant has no channel at all, the alert
// has already reached its reader on the page, so it is marked sent and the
// queue stays honest. If a channel is configured but the send fails, the alert
// stays pending with the error kept, until the store's retry budget runs out.
func (s *Sender) DeliverPending(ctx context.Context, tenantID uuid.UUID) (sent int, err error) {
	settings, err := s.Store.SettingsFor(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	pending, err := s.Store.PendingAlerts(ctx, tenantID, 100)
	if err != nil {
		return 0, err
	}

	for _, a := range pending {
		if err := s.deliverOne(ctx, tenantID, settings, a); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func (s *Sender) deliverOne(ctx context.Context, tenantID uuid.UUID, settings store.Settings, a store.Alert) error {
	to := strings.TrimSpace(settings.AlertEmail)
	chat := strings.TrimSpace(settings.AlertTelegramChat)

	// Nothing to push to: the page has it, and pretending it is queued
	// forever would make the queue a lie.
	if to == "" && chat == "" {
		return s.Store.MarkAlertSent(ctx, tenantID, a.ID)
	}

	subject, body := s.render(a, settings.CompanyName)

	var firstErr error
	if to != "" && s.Mail != nil {
		if err := s.Mail.Send(ctx, to, subject, body); err != nil {
			firstErr = err
		}
	}
	if chat != "" && s.Notifier != nil {
		if err := s.Notifier.Send(ctx, chat, subject+"\n"+body); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return s.Store.MarkAlertFailed(ctx, tenantID, a.ID, firstErr.Error())
	}
	return s.Store.MarkAlertSent(ctx, tenantID, a.ID)
}

// render builds the subject and body an owner reads. The kind is spelled out
// rather than shown as its code, because nobody at a dealership reads
// "NUMBERING_GAP" as a sentence.
func (s *Sender) render(a store.Alert, company string) (subject, body string) {
	kind := map[string]string{
		store.AlertVoid:         "A document was voided",
		store.AlertVariance:     "The drawer is past its limit",
		store.AlertNumberingGap: "A document number is missing",
		store.AlertOverride:     "A limit was overridden",
		store.AlertAfterHours:   "Something happened after hours",
	}[a.Kind]
	if kind == "" {
		kind = "An alert fired"
	}

	who := strings.TrimSpace(company)
	if who != "" {
		who = " at " + who
	}
	subject = kind + who
	body = a.Message + "\n\n" + s.now().UTC().Format("2006-01-02 15:04 UTC")
	if a.Entity != "" {
		body += "\nReference: " + a.Entity
	}
	return subject, body
}

func (s *Sender) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// SMTPMailer sends mail through a relay with net/smtp. It is the whole mail
// client this app needs: one relay, one login, plain text.
type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	// From is the address the mail is sent as. The relay usually insists it
	// is one of its own.
	From string
}

// Available reports whether the relay is configured enough to try.
func (m SMTPMailer) Available() bool {
	return m.Host != "" && m.Port > 0 && m.From != ""
}

func (m SMTPMailer) Send(ctx context.Context, to, subject, body string) error {
	if !m.Available() {
		return fmt.Errorf("smtp is not configured")
	}
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	from := m.From
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}
	// smtp.SendMail opens the socket itself, so the context bounds the whole
	// call rather than only the dial.
	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("smtp send: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TelegramNotifier posts to the bot API. It is deliberately the smallest
// client that works: one chat id, one message, no retries (the store owns
// the retry budget).
type TelegramNotifier struct {
	Token string
	// HTTP is the client used for the call. The zero value uses a client with
	// a ten second timeout, which is long enough for one message and short
	// enough not to hold a request open.
	HTTP *http.Client
}

// Available reports whether a bot token is configured.
func (t TelegramNotifier) Available() bool {
	return t.Token != ""
}

func (t TelegramNotifier) Send(ctx context.Context, chatID, text string) error {
	if !t.Available() {
		return fmt.Errorf("telegram is not configured")
	}
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	form := strings.NewReader("chat_id=" + url.QueryEscape(chatID) + "&text=" + url.QueryEscape(text))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+t.Token+"/sendMessage", form)
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The body carries Telegram's own reason, which is what the owner
		// needs to see on the alerts page.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("telegram: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
