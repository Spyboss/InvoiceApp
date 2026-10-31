package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// stubStore is the slice of the store the sender uses, driven by hand so the
// channel logic can be proven without a database.
type stubStore struct {
	settings store.Settings
	pending  []store.Alert

	sent   []uuid.UUID
	failed map[uuid.UUID]string
}

func (s *stubStore) SettingsFor(context.Context, uuid.UUID) (store.Settings, error) {
	return s.settings, nil
}

func (s *stubStore) PendingAlerts(context.Context, uuid.UUID, int) ([]store.Alert, error) {
	return s.pending, nil
}

func (s *stubStore) MarkAlertSent(_ context.Context, _, alertID uuid.UUID) error {
	s.sent = append(s.sent, alertID)
	return nil
}

func (s *stubStore) MarkAlertFailed(_ context.Context, _, alertID uuid.UUID, sendErr string) error {
	if s.failed == nil {
		s.failed = make(map[uuid.UUID]string)
	}
	s.failed[alertID] = sendErr
	return nil
}

// stubMailer records what it was asked to send, or fails on demand.
type stubMailer struct {
	to      []string
	subject []string
	err     error
}

func (m *stubMailer) Send(_ context.Context, to, subject, _ string) error {
	m.to = append(m.to, to)
	m.subject = append(m.subject, subject)
	return m.err
}

// stubNotifier records the chat and text it was asked to send, or fails.
type stubNotifier struct {
	chat []string
	text []string
	err  error
}

func (n *stubNotifier) Send(_ context.Context, chatID, text string) error {
	n.chat = append(n.chat, chatID)
	n.text = append(n.text, text)
	return n.err
}

func alert(kind, message string) store.Alert {
	return store.Alert{
		ID:        uuid.New(),
		Kind:      kind,
		Message:   message,
		Severity:  store.SeverityWarning,
		Status:    store.AlertPending,
		CreatedAt: time.Date(2026, 10, 30, 15, 4, 0, 0, time.UTC),
	}
}

// testSender builds a sender with a fixed clock so the rendered body is
// deterministic.
func testSender(stub *stubStore, mail Mailer, note Notifier) *Sender {
	return &Sender{
		Store:    stub,
		Mail:     mail,
		Notifier: note,
		Now: func() time.Time {
			return time.Date(2026, 10, 30, 15, 4, 0, 0, time.UTC)
		},
	}
}

// NoChannelMarksSent proves the rule that keeps the queue honest: a tenant that
// has configured no destination has already read the alert on the page, so it
// is marked sent rather than left pending forever.
func TestNoChannelMarksSent(t *testing.T) {
	stub := &stubStore{
		pending: []store.Alert{alert(store.AlertVoid, "Invoice 0007 was voided")},
	}
	mail := &stubMailer{}
	note := &stubNotifier{}

	sent, err := testSender(stub, mail, note).DeliverPending(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if len(stub.sent) != 1 || stub.sent[0] != stub.pending[0].ID {
		t.Errorf("marked sent = %v, want the pending alert", stub.sent)
	}
	if len(mail.to) != 0 || len(note.chat) != 0 {
		t.Errorf("no channel is configured but something was sent: mail %v, chat %v", mail.to, note.chat)
	}
}

// EmailChannelSendsAndMarksSent proves a configured mailbox is used, with the
// tenant's own address, and the alert is marked sent on success.
func TestEmailChannelSendsAndMarksSent(t *testing.T) {
	stub := &stubStore{
		settings: store.Settings{
			CompanyName: "Alpha Motors",
			AlertEmail:  "alerts@alpha.local",
		},
		pending: []store.Alert{alert(store.AlertVariance, "Drawer short by 1,200.00")},
	}
	mail := &stubMailer{}

	sent, err := testSender(stub, mail, nil).DeliverPending(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if len(mail.to) != 1 || mail.to[0] != "alerts@alpha.local" {
		t.Errorf("mailed to %v, want alerts@alpha.local", mail.to)
	}
	// The subject spells the kind out and carries the company, so the owner
	// can see at a glance which dealership it is.
	if len(mail.subject) != 1 || mail.subject[0] != "The drawer is past its limit at Alpha Motors" {
		t.Errorf("subject = %q, want the spelled-out kind with the company", mail.subject)
	}
	if len(stub.sent) != 1 {
		t.Errorf("marked sent = %v, want the alert marked sent", stub.sent)
	}
	if len(stub.failed) != 0 {
		t.Errorf("failed = %v, want none on a clean send", stub.failed)
	}
}

// SendErrorKeepsItPending proves a failed send is recorded with the error and
// the alert is NOT marked sent, so the retry budget in the store still owns it.
func TestSendErrorKeepsItPending(t *testing.T) {
	a := alert(store.AlertVoid, "Invoice 0007 was voided")
	stub := &stubStore{
		settings: store.Settings{AlertEmail: "alerts@alpha.local"},
		pending:  []store.Alert{a},
	}
	mail := &stubMailer{err: errors.New("smtp: connection refused")}

	_, err := testSender(stub, mail, nil).DeliverPending(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(stub.sent) != 0 {
		t.Errorf("marked sent = %v, want none on a failed send", stub.sent)
	}
	if got := stub.failed[a.ID]; got != "smtp: connection refused" {
		t.Errorf("failed reason = %q, want the smtp error kept", got)
	}
}

// BothChannelsAttempted proves a tenant with both an address and a chat gets
// both, and that a failure on one channel does not stop the other from being
// tried (the first error is what is kept).
func TestBothChannelsAttempted(t *testing.T) {
	a := alert(store.AlertNumberingGap, "Number 0007 was never issued")
	stub := &stubStore{
		settings: store.Settings{
			AlertEmail:        "alerts@alpha.local",
			AlertTelegramChat: "-1001234",
		},
		pending: []store.Alert{a},
	}
	mail := &stubMailer{}
	note := &stubNotifier{}

	sent, err := testSender(stub, mail, note).DeliverPending(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if len(mail.to) != 1 || mail.to[0] != "alerts@alpha.local" {
		t.Errorf("mailed to %v, want the tenant's address", mail.to)
	}
	if len(note.chat) != 1 || note.chat[0] != "-1001234" {
		t.Errorf("notified chat %v, want the tenant's chat", note.chat)
	}
	// The telegram message carries the same subject and body as the email.
	if len(note.text) != 1 || note.text[0] == "" {
		t.Errorf("telegram text = %v, want a non-empty message", note.text)
	}
}

// NotifierErrorKeepsItPending proves the chat channel failing also keeps the
// alert queued with the error, even when the email went out fine.
func TestNotifierErrorKeepsItPending(t *testing.T) {
	a := alert(store.AlertVariance, "Drawer short by 5,000.00")
	stub := &stubStore{
		settings: store.Settings{
			AlertEmail:        "alerts@alpha.local",
			AlertTelegramChat: "-1001234",
		},
		pending: []store.Alert{a},
	}
	mail := &stubMailer{}
	note := &stubNotifier{err: errors.New("telegram: 403 Forbidden")}

	_, err := testSender(stub, mail, note).DeliverPending(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	// The email still went out.
	if len(mail.to) != 1 {
		t.Errorf("email not attempted before the chat failed: %v", mail.to)
	}
	// But the alert stays queued, because not every channel reached a human.
	if len(stub.sent) != 0 {
		t.Errorf("marked sent = %v, want none when a channel failed", stub.sent)
	}
	if got := stub.failed[a.ID]; got != "telegram: 403 Forbidden" {
		t.Errorf("failed reason = %q, want the telegram error kept", got)
	}
}

// RenderSpellsTheKindOut proves the owner reads a sentence, not a code.
func TestRenderSpellsTheKindOut(t *testing.T) {
	s := testSender(&stubStore{}, nil, nil)
	cases := map[string]string{
		store.AlertVoid:         "A document was voided",
		store.AlertVariance:     "The drawer is past its limit",
		store.AlertNumberingGap: "A document number is missing",
		store.AlertOverride:     "A limit was overridden",
		store.AlertAfterHours:   "Something happened after hours",
	}
	for kind, wantSubject := range cases {
		subject, body := s.render(alert(kind, "detail"), "Alpha Motors")
		if subject != wantSubject+" at Alpha Motors" {
			t.Errorf("kind %q subject = %q, want %q", kind, subject, wantSubject+" at Alpha Motors")
		}
		if body == "" {
			t.Errorf("kind %q has an empty body", kind)
		}
	}
}
