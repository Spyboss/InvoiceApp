package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// How the money arrived. Only cash sits in the drawer at day end, so the
// method decides what the z report expects to count.
const (
	MethodCash   = "CASH"
	MethodCard   = "CARD"
	MethodBank   = "BANK"
	MethodCheque = "CHEQUE"
)

// PaymentMethods is the order a counter offers them in.
var PaymentMethods = []string{MethodCash, MethodCard, MethodBank, MethodCheque}

// ValidPaymentMethod reports whether the method is one the business takes.
func ValidPaymentMethod(m string) bool {
	switch m {
	case MethodCash, MethodCard, MethodBank, MethodCheque:
		return true
	}
	return false
}

// A receipt is posted when it is written and reversed when it is withdrawn.
// There is no third state and no delete.
const (
	PaymentPosted   = "POSTED"
	PaymentReversed = "REVERSED"
)

// Payment is one receipt against one invoice. The invoice keeps what it was
// issued with; this row is the record of money arriving, or of that money
// being handed back.
type Payment struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	DocumentID  uuid.UUID
	ReceiptNo   string
	ReceiptYear int
	PaidOn      time.Time
	Method      string
	AmountCents int64
	Reference   string
	Status      string
	Notes       string

	ReversalReason string
	ReversedOn     *time.Time
	ReversedBy     *uuid.UUID
	ReversedAt     *time.Time

	CreatedBy *uuid.UUID
	CreatedAt time.Time

	// Read from the invoice and its people, not stored on the receipt.
	DocNo          string
	DocType        string
	CustomerName   string
	CreatedByName  *string
	ReversedByName *string
}

const paymentColumns = `p.id, p.tenant_id, p.document_id, p.receipt_no, p.receipt_year, p.paid_on,
	p.method, p.amount_cents, p.reference, p.status, p.notes, p.reversal_reason, p.reversed_on,
	p.reversed_by, p.reversed_at, p.created_by, p.created_at,
	d.doc_no, d.doc_type, d.customer_name, uc.name, ur.name`

const paymentFrom = `
	from payments p
	join documents d on d.id = p.document_id
	left join users uc on uc.id = p.created_by
	left join users ur on ur.id = p.reversed_by`

// PaymentParams is what the cashier captured at the counter.
type PaymentParams struct {
	DocumentID  uuid.UUID
	PaidOn      time.Time
	Method      string
	AmountCents int64
	Reference   string
	Notes       string
}

// DocumentBalance is what an invoice was issued for, what has been paid
// against it, and what is left. The balance never changes on the document, so
// this is derived every time it is asked for.
type DocumentBalance struct {
	DocNo            string
	DocType          string
	BalanceCents     int64
	PaidCents        int64
	OutstandingCents int64
}

// CreatePayment posts one receipt against an invoice. It locks the invoice,
// checks it against what is still owed, and only then takes a receipt number,
// so a refused payment does not burn a number. Must run inside InTenant.
func (s *Store) CreatePayment(ctx context.Context, tenantID, createdBy uuid.UUID, p PaymentParams) (Payment, error) {
	reference := strings.TrimSpace(p.Reference)
	notes := strings.TrimSpace(p.Notes)

	switch {
	case p.DocumentID == uuid.Nil, p.PaidOn.IsZero(), p.AmountCents <= 0:
		return Payment{}, ErrInvalidPayment
	case !ValidPaymentMethod(p.Method):
		return Payment{}, ErrInvalidPayment
	case utf8.RuneCountInString(reference) > 60, utf8.RuneCountInString(notes) > 2000:
		return Payment{}, ErrInvalidPayment
	}

	closed, err := s.closedOn(ctx, tenantID, p.PaidOn)
	if err != nil {
		return Payment{}, err
	}
	if closed {
		return Payment{}, ErrDayAlreadyClosed
	}

	balance, err := s.documentBalanceLocked(ctx, tenantID, p.DocumentID)
	if err != nil {
		return Payment{}, err
	}
	if balance.DocType != DocSalesCash && balance.DocType != DocSalesLease {
		return Payment{}, ErrInvalidPayment
	}
	if balance.OutstandingCents <= 0 || p.AmountCents > balance.OutstandingCents {
		return Payment{}, ErrPaymentOverBalance
	}

	receiptNo, err := s.AllocateNo(ctx, tenantID, SeriesPayment, p.PaidOn.Year())
	if err != nil {
		return Payment{}, err
	}

	const q = `
		insert into payments (tenant_id, document_id, receipt_no, receipt_year, paid_on,
			method, amount_cents, reference, notes, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		returning id`
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, q, tenantID, p.DocumentID, receiptNo, p.PaidOn.Year(), p.PaidOn,
		p.Method, p.AmountCents, reference, notes, createdBy).Scan(&id); err != nil {
		// The trigger is the last word on the balance; the check above is only
		// there to give the clerk a readable reason.
		if sqlState(err) == "PAY02" {
			return Payment{}, ErrPaymentOverBalance
		}
		if sqlState(err) == "PAY01" {
			return Payment{}, ErrInvalidPayment
		}
		if sqlState(err) == "DAY01" {
			return Payment{}, ErrDayAlreadyClosed
		}
		return Payment{}, fmt.Errorf("create payment: %w", err)
	}
	return s.PaymentByID(ctx, tenantID, id)
}

// closedOn reports whether a business date has already been closed. Money is
// never dated on a closed day: a receipt taken after the z would land on a date
// whose totals are frozen, so it would be on no z at all. Must run inside
// InTenant.
func (s *Store) closedOn(ctx context.Context, tenantID uuid.UUID, date time.Time) (bool, error) {
	const q = `select exists (select 1 from day_ends where tenant_id = $1 and business_date = $2)`
	var closed bool
	if err := s.db.QueryRow(ctx, q, tenantID, date).Scan(&closed); err != nil {
		return false, fmt.Errorf("closed day check: %w", err)
	}
	return closed, nil
}

// ReversePayment withdraws a posted receipt. The money is dated on the day the
// reversal happens, not on the day it was taken, so a day that was already
// counted and closed never changes. Must run inside InTenant.
func (s *Store) ReversePayment(ctx context.Context, tenantID, paymentID, reversedBy uuid.UUID, reason string, reversedOn time.Time) (Payment, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 500 || reversedOn.IsZero() {
		return Payment{}, ErrInvalidPayment
	}

	closed, err := s.closedOn(ctx, tenantID, reversedOn)
	if err != nil {
		return Payment{}, err
	}
	if closed {
		return Payment{}, ErrDayAlreadyClosed
	}

	const q = `
		update payments
		set status = 'REVERSED', reversal_reason = $4, reversed_on = $5, reversed_by = $6,
		    reversed_at = now(), updated_at = now()
		where tenant_id = $1 and id = $2 and status = $3
		returning id`
	var id uuid.UUID
	err = s.db.QueryRow(ctx, q, tenantID, paymentID, PaymentPosted, reason, reversedOn, reversedBy).Scan(&id)
	if err != nil {
		if notFound(err) == ErrNotFound {
			// Either there is no such receipt or it is already reversed; the
			// difference matters to the person looking at it.
			if _, lookErr := s.PaymentByID(ctx, tenantID, paymentID); lookErr == nil {
				return Payment{}, ErrPaymentNotReversible
			}
			return Payment{}, ErrNotFound
		}
		if sqlState(err) == "PAY03" {
			return Payment{}, ErrPaymentNotReversible
		}
		if sqlState(err) == "DAY01" {
			return Payment{}, ErrDayAlreadyClosed
		}
		return Payment{}, fmt.Errorf("reverse payment: %w", err)
	}
	return s.PaymentByID(ctx, tenantID, id)
}

// PaymentByID must run inside InTenant.
func (s *Store) PaymentByID(ctx context.Context, tenantID, id uuid.UUID) (Payment, error) {
	q := `select ` + paymentColumns + paymentFrom + `
		where p.tenant_id = $1 and p.id = $2`
	return scanPayment(s.db.QueryRow(ctx, q, tenantID, id))
}

// PaymentFilter narrows the receipt list.
type PaymentFilter struct {
	DocumentID *uuid.UUID
	Method     string
	From       time.Time
	To         time.Time
	Search     string
	Limit      int
}

// ListPayments must run inside InTenant.
func (s *Store) ListPayments(ctx context.Context, tenantID uuid.UUID, f PaymentFilter) ([]Payment, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	q := `select ` + paymentColumns + paymentFrom + `
		where p.tenant_id = $1
		  and ($2::uuid is null or p.document_id = $2)
		  and ($3 = '' or p.method = $3)
		  and ($4::date is null or p.paid_on >= $4)
		  and ($5::date is null or p.paid_on <= $5)
		  and ($6 = '' or p.receipt_no ilike '%' || $6 || '%' or d.doc_no ilike '%' || $6 || '%'
		       or d.customer_name ilike '%' || $6 || '%' or p.reference ilike '%' || $6 || '%')
		order by p.paid_on desc, p.receipt_year desc, p.receipt_no desc
		limit $7`

	var from, to *time.Time
	if !f.From.IsZero() {
		from = &f.From
	}
	if !f.To.IsZero() {
		to = &f.To
	}

	rows, err := s.db.Query(ctx, q, tenantID, f.DocumentID, f.Method, from, to, f.Search, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()

	out := make([]Payment, 0, f.Limit)
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	return out, nil
}

// DocumentBalanceFor reads what an invoice still owes, without locking it.
// Must run inside InTenant.
func (s *Store) DocumentBalanceFor(ctx context.Context, tenantID, documentID uuid.UUID) (DocumentBalance, error) {
	const q = `
		select d.doc_no, d.doc_type, d.balance_cents,
		       coalesce((select sum(p.amount_cents) from payments p
		                 where p.tenant_id = $1 and p.document_id = d.id and p.status = 'POSTED'), 0)
		from documents d
		where d.tenant_id = $1 and d.id = $2`

	var b DocumentBalance
	if err := s.db.QueryRow(ctx, q, tenantID, documentID).Scan(&b.DocNo, &b.DocType, &b.BalanceCents, &b.PaidCents); err != nil {
		return DocumentBalance{}, notFound(err)
	}
	b.OutstandingCents = b.BalanceCents - b.PaidCents
	return b, nil
}

// documentBalanceLocked holds the invoice row for the rest of the transaction,
// so two receipts posted at the same moment cannot both pass against the same
// balance. Must run inside InTenant.
func (s *Store) documentBalanceLocked(ctx context.Context, tenantID, documentID uuid.UUID) (DocumentBalance, error) {
	const lock = `select doc_no, doc_type, balance_cents
		from documents where tenant_id = $1 and id = $2 for update`

	var b DocumentBalance
	if err := s.db.QueryRow(ctx, lock, tenantID, documentID).Scan(&b.DocNo, &b.DocType, &b.BalanceCents); err != nil {
		return DocumentBalance{}, notFound(err)
	}

	const paid = `select coalesce(sum(amount_cents), 0) from payments
		where tenant_id = $1 and document_id = $2 and status = 'POSTED'`
	if err := s.db.QueryRow(ctx, paid, tenantID, documentID).Scan(&b.PaidCents); err != nil {
		return DocumentBalance{}, fmt.Errorf("paid so far: %w", err)
	}

	b.OutstandingCents = b.BalanceCents - b.PaidCents
	return b, nil
}

// Receivable is one invoice that is still owed something, as at a business
// date. The receivables report ages them.
type Receivable struct {
	DocumentID       uuid.UUID
	DocNo            string
	DocDate          time.Time
	CustomerName     string
	FinanceCompany   string
	BalanceCents     int64
	PaidCents        int64
	OutstandingCents int64
	Days             int
}

// ReceivablesFor lists unpaid invoices as at the given business date, oldest
// first so the report reads like a reminder list. A clerk who may not read
// every document passes their own id and sees only what they sold. Must run
// inside InTenant.
func (s *Store) ReceivablesFor(ctx context.Context, tenantID uuid.UUID, asOf time.Time, createdBy *uuid.UUID, limit int) ([]Receivable, error) {
	if limit <= 0 {
		limit = 500
	}
	const q = `
		select d.id, d.doc_no, d.doc_date, d.customer_name, d.finance_company,
		       d.balance_cents, coalesce(p.paid_cents, 0),
		       d.balance_cents - coalesce(p.paid_cents, 0) as outstanding,
		       ($3::date - d.doc_date) as days
		from documents d
		left join (
		    -- Money counts as paid from the day it was taken, and a reversal
		    -- only stops counting it from the day the money went back. So a
		    -- report for last month reads as it read last month, even if the
		    -- receipt was reversed since.
		    select document_id, sum(amount_cents) as paid_cents
		    from payments
		    where tenant_id = $1
		      and paid_on <= $3
		      and (status = 'POSTED' or reversed_on > $3)
		    group by document_id
		) p on p.document_id = d.id
		where d.tenant_id = $1
		  and d.doc_type in ('SALES_CASH', 'SALES_LEASING')
		  and d.doc_date <= $3
		  and d.balance_cents - coalesce(p.paid_cents, 0) > 0
		  and ($4::uuid is null or d.created_by = $4)
		order by d.doc_date, d.doc_year, d.doc_no
		limit $2`

	rows, err := s.db.Query(ctx, q, tenantID, limit, asOf, createdBy)
	if err != nil {
		return nil, fmt.Errorf("list receivables: %w", err)
	}
	defer rows.Close()

	out := make([]Receivable, 0, limit)
	for rows.Next() {
		var r Receivable
		if err := rows.Scan(&r.DocumentID, &r.DocNo, &r.DocDate, &r.CustomerName, &r.FinanceCompany,
			&r.BalanceCents, &r.PaidCents, &r.OutstandingCents, &r.Days); err != nil {
			return nil, fmt.Errorf("scan receivable: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list receivables: %w", err)
	}
	return out, nil
}

func scanPayment(row rowScanner) (Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.TenantID, &p.DocumentID, &p.ReceiptNo, &p.ReceiptYear, &p.PaidOn,
		&p.Method, &p.AmountCents, &p.Reference, &p.Status, &p.Notes, &p.ReversalReason, &p.ReversedOn,
		&p.ReversedBy, &p.ReversedAt, &p.CreatedBy, &p.CreatedAt,
		&p.DocNo, &p.DocType, &p.CustomerName, &p.CreatedByName, &p.ReversedByName)
	if err != nil {
		return Payment{}, fmt.Errorf("scan payment: %w", notFound(err))
	}
	return p, nil
}
