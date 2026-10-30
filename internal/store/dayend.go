package store

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The sections of a closed day, in the order the z report prints them.
const (
	DayPayments  = "PAYMENT"
	DayReversals = "REVERSAL"
	DayDocuments = "DOCUMENT"
	DayStock     = "STOCK"
)

// DayWindow is one business day on the dealership's calendar. Paper carries
// business dates; the stock ledger only has instants, so the window carries
// both: Date for the date columns, Start and End for anything timestamped.
type DayWindow struct {
	Date  time.Time
	Start time.Time
	End   time.Time
}

// DayEndLine is one frozen line of a z report. Section says which table it
// belongs to and Label is the code stored in the database (CASH, SALES_CASH,
// SELL), never a translated string, so a later wording change cannot rewrite
// what the paper said.
type DayEndLine struct {
	Section     string
	Label       string
	Qty         int
	AmountCents int64
	Position    int
}

// DayEnd is one closed day: the cash the drawer was expected to hold, what the
// cashier counted, the difference, and the lines that were printed.
type DayEnd struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	BusinessDate time.Time
	ZNo          string
	ZYear        int

	OpeningFloatCents  int64
	CashInCents        int64
	CashOutCents       int64
	ExpectedCents      int64
	CountedCents       int64
	VarianceCents      int64
	SalesCount         int
	SalesTotalCents    int64
	PaymentsTotalCents int64

	Notes        string
	ClosedBy     uuid.UUID
	ClosedByName *string
	ClosedAt     time.Time

	Lines []DayEndLine
}

// Short reports whether the drawer was short, rather than over or exact.
func (d DayEnd) Short() bool { return d.VarianceCents < 0 }

// DayTotals is everything that happened on one business date, read from the
// payments, documents and stock movements dated on it. It is both the preview
// the cashier checks before closing and the numbers the close stores.
type DayTotals struct {
	CashInCents        int64
	CashOutCents       int64
	SalesCount         int
	SalesTotalCents    int64
	PaymentsTotalCents int64

	Payments  []DayEndLine
	Reversals []DayEndLine
	Documents []DayEndLine
	Stock     []DayEndLine
}

// ExpectedCash is C7: the float the day opened with, plus the cash taken,
// minus the cash handed back.
func (t DayTotals) ExpectedCash(openingFloatCents int64) int64 {
	return openingFloatCents + t.CashInCents - t.CashOutCents
}

// Lines flattens the four sections into the print order, numbering them so a
// closed day reads back exactly as it was stored.
func (t DayTotals) Lines() []DayEndLine {
	out := make([]DayEndLine, 0, len(t.Payments)+len(t.Reversals)+len(t.Documents)+len(t.Stock))
	for _, section := range [][]DayEndLine{t.Payments, t.Reversals, t.Documents, t.Stock} {
		for _, line := range section {
			line.Position = len(out)
			out = append(out, line)
		}
	}
	return out
}

// DayTotalsFor reads the day's payments by method, reversals by method,
// documents by type and stock movements by kind. Must run inside InTenant.
func (s *Store) DayTotalsFor(ctx context.Context, tenantID uuid.UUID, w DayWindow) (DayTotals, error) {
	var t DayTotals

	payments, err := s.dayMethodLines(ctx, tenantID, w.Date, "paid_on", DayPayments)
	if err != nil {
		return DayTotals{}, err
	}
	t.Payments = payments
	for _, line := range payments {
		t.PaymentsTotalCents += line.AmountCents
		if line.Label == MethodCash {
			t.CashInCents += line.AmountCents
		}
	}

	reversals, err := s.dayMethodLines(ctx, tenantID, w.Date, "reversed_on", DayReversals)
	if err != nil {
		return DayTotals{}, err
	}
	t.Reversals = reversals
	for _, line := range reversals {
		if line.Label == MethodCash {
			t.CashOutCents += line.AmountCents
		}
	}

	documents, err := s.dayDocumentLines(ctx, tenantID, w.Date)
	if err != nil {
		return DayTotals{}, err
	}
	t.Documents = documents
	for _, line := range documents {
		if line.Label == DocSalesCash || line.Label == DocSalesLease {
			t.SalesCount += line.Qty
			t.SalesTotalCents += line.AmountCents
		}
	}

	stock, err := s.dayStockLines(ctx, tenantID, w)
	if err != nil {
		return DayTotals{}, err
	}
	t.Stock = stock

	return t, nil
}

// dayMethodLines groups receipts or reversals on one date by method. The
// column is chosen by the caller from a constant, never from input.
func (s *Store) dayMethodLines(ctx context.Context, tenantID uuid.UUID, day time.Time, dateColumn, section string) ([]DayEndLine, error) {
	q := `
		select method, count(*), coalesce(sum(amount_cents), 0)
		from payments
		where tenant_id = $1 and ` + dateColumn + ` = $2
		group by method
		order by method`

	rows, err := s.db.Query(ctx, q, tenantID, day)
	if err != nil {
		return nil, fmt.Errorf("day %s: %w", dateColumn, err)
	}
	defer rows.Close()

	out, err := scanDayLines(rows, section)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) dayDocumentLines(ctx context.Context, tenantID uuid.UUID, day time.Time) ([]DayEndLine, error) {
	// Invoices first, then the paper that only offers or holds a unit: that is
	// the order the cashier reads them in.
	const q = `
		select doc_type, count(*), coalesce(sum(price_cents), 0)
		from documents
		where tenant_id = $1 and doc_date = $2
		group by doc_type
		order by case doc_type
			when 'SALES_CASH' then 1
			when 'SALES_LEASING' then 2
			when 'ADVANCE' then 3
			when 'PROFORMA' then 4
			else 5 end`

	rows, err := s.db.Query(ctx, q, tenantID, day)
	if err != nil {
		return nil, fmt.Errorf("day documents: %w", err)
	}
	defer rows.Close()

	return scanDayLines(rows, DayDocuments)
}

func (s *Store) dayStockLines(ctx context.Context, tenantID uuid.UUID, w DayWindow) ([]DayEndLine, error) {
	const q = `
		select kind, count(*), 0
		from stock_movements
		where tenant_id = $1 and created_at >= $2 and created_at < $3
		group by kind
		order by case kind when 'RECEIVE' then 1 when 'SELL' then 2 when 'RETURN' then 3 else 4 end`

	rows, err := s.db.Query(ctx, q, tenantID, w.Start, w.End)
	if err != nil {
		return nil, fmt.Errorf("day stock movements: %w", err)
	}
	defer rows.Close()

	return scanDayLines(rows, DayStock)
}

func scanDayLines(rows pgx.Rows, section string) ([]DayEndLine, error) {
	var out []DayEndLine
	for rows.Next() {
		var line DayEndLine
		line.Section = section
		if err := rows.Scan(&line.Label, &line.Qty, &line.AmountCents); err != nil {
			return nil, fmt.Errorf("scan day line: %w", err)
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read day lines: %w", err)
	}
	return out, nil
}

// DayEndParams is what the cashier typed at close: the float the day started
// with, what they counted, and anything worth writing down.
type DayEndParams struct {
	Window            DayWindow
	OpeningFloatCents int64
	CountedCents      int64
	Notes             string
}

// CloseDay computes the day's totals, stores them with the variance, and
// freezes the lines the z report prints. It refuses a day that is already
// closed, and the row it writes can never be updated. Must run inside
// InTenant.
func (s *Store) CloseDay(ctx context.Context, tenantID, closedBy uuid.UUID, p DayEndParams) (DayEnd, error) {
	notes := p.Notes
	if p.Window.Date.IsZero() || p.OpeningFloatCents < 0 || p.CountedCents < 0 ||
		utf8.RuneCountInString(notes) > 2000 {
		return DayEnd{}, ErrInvalidDayEnd
	}

	totals, err := s.DayTotalsFor(ctx, tenantID, p.Window)
	if err != nil {
		return DayEnd{}, err
	}

	expected := totals.ExpectedCash(p.OpeningFloatCents)
	variance := p.CountedCents - expected

	zNo, err := s.AllocateNo(ctx, tenantID, SeriesDayEnd, p.Window.Date.Year())
	if err != nil {
		return DayEnd{}, err
	}

	const q = `
		insert into day_ends (tenant_id, business_date, z_no, z_year,
			opening_float_cents, cash_in_cents, cash_out_cents, expected_cents,
			counted_cents, variance_cents, sales_count, sales_total_cents,
			payments_total_cents, notes, closed_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		returning id`

	var id uuid.UUID
	err = s.db.QueryRow(ctx, q, tenantID, p.Window.Date, zNo, p.Window.Date.Year(),
		p.OpeningFloatCents, totals.CashInCents, totals.CashOutCents, expected,
		p.CountedCents, variance, totals.SalesCount, totals.SalesTotalCents,
		totals.PaymentsTotalCents, notes, closedBy).Scan(&id)
	if constraintName(err) == "day_ends_once_per_day" {
		return DayEnd{}, ErrDayAlreadyClosed
	}
	if err != nil {
		return DayEnd{}, fmt.Errorf("close day: %w", err)
	}

	lines := totals.Lines()
	if len(lines) > 0 {
		const insertLine = `
			insert into day_end_lines (tenant_id, day_end_id, section, label, qty, amount_cents, position)
			values ($1, $2, $3, $4, $5, $6, $7)`
		for _, line := range lines {
			if _, err := s.db.Exec(ctx, insertLine, tenantID, id, line.Section, line.Label,
				line.Qty, line.AmountCents, line.Position); err != nil {
				return DayEnd{}, fmt.Errorf("store day end line: %w", err)
			}
		}
	}

	return s.DayEndByID(ctx, tenantID, id)
}

// DayEndByID must run inside InTenant.
func (s *Store) DayEndByID(ctx context.Context, tenantID, id uuid.UUID) (DayEnd, error) {
	const q = `
		select d.id, d.tenant_id, d.business_date, d.z_no, d.z_year,
		       d.opening_float_cents, d.cash_in_cents, d.cash_out_cents, d.expected_cents,
		       d.counted_cents, d.variance_cents, d.sales_count, d.sales_total_cents,
		       d.payments_total_cents, d.notes, d.closed_by, u.name, d.closed_at
		from day_ends d
		left join users u on u.id = d.closed_by
		where d.tenant_id = $1 and d.id = $2`

	end, err := scanDayEnd(s.db.QueryRow(ctx, q, tenantID, id))
	if err != nil {
		return DayEnd{}, err
	}

	const lines = `
		select section, label, qty, amount_cents, position
		from day_end_lines
		where tenant_id = $1 and day_end_id = $2
		order by position`
	rows, err := s.db.Query(ctx, lines, tenantID, id)
	if err != nil {
		return DayEnd{}, fmt.Errorf("day end lines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var line DayEndLine
		if err := rows.Scan(&line.Section, &line.Label, &line.Qty, &line.AmountCents, &line.Position); err != nil {
			return DayEnd{}, fmt.Errorf("scan day end line: %w", err)
		}
		end.Lines = append(end.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return DayEnd{}, fmt.Errorf("day end lines: %w", err)
	}
	return end, nil
}

// DayEndOnDate finds the close for a business date, or ErrNotFound when the
// day is still open. Must run inside InTenant.
func (s *Store) DayEndOnDate(ctx context.Context, tenantID uuid.UUID, date time.Time) (DayEnd, error) {
	const q = `select id from day_ends where tenant_id = $1 and business_date = $2`
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, q, tenantID, date).Scan(&id); err != nil {
		return DayEnd{}, notFound(err)
	}
	return s.DayEndByID(ctx, tenantID, id)
}

// ListDayEnds returns the recent closes, newest first, without their lines.
// Must run inside InTenant.
func (s *Store) ListDayEnds(ctx context.Context, tenantID uuid.UUID, limit int) ([]DayEnd, error) {
	if limit <= 0 {
		limit = 60
	}
	const q = `
		select d.id, d.tenant_id, d.business_date, d.z_no, d.z_year,
		       d.opening_float_cents, d.cash_in_cents, d.cash_out_cents, d.expected_cents,
		       d.counted_cents, d.variance_cents, d.sales_count, d.sales_total_cents,
		       d.payments_total_cents, d.notes, d.closed_by, u.name, d.closed_at
		from day_ends d
		left join users u on u.id = d.closed_by
		where d.tenant_id = $1
		order by d.business_date desc
		limit $2`

	rows, err := s.db.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list day ends: %w", err)
	}
	defer rows.Close()

	out := make([]DayEnd, 0, limit)
	for rows.Next() {
		end, err := scanDayEnd(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, end)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list day ends: %w", err)
	}
	return out, nil
}

// LastCountedCash is what the drawer held at the previous close, which is the
// best guess at the float the next day opens with. Must run inside InTenant.
func (s *Store) LastCountedCash(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	const q = `select counted_cents from day_ends
		where tenant_id = $1 order by business_date desc limit 1`

	var counted int64
	err := s.db.QueryRow(ctx, q, tenantID).Scan(&counted)
	if notFound(err) == ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("last counted cash: %w", err)
	}
	return counted, nil
}

func scanDayEnd(row rowScanner) (DayEnd, error) {
	var d DayEnd
	err := row.Scan(&d.ID, &d.TenantID, &d.BusinessDate, &d.ZNo, &d.ZYear,
		&d.OpeningFloatCents, &d.CashInCents, &d.CashOutCents, &d.ExpectedCents,
		&d.CountedCents, &d.VarianceCents, &d.SalesCount, &d.SalesTotalCents,
		&d.PaymentsTotalCents, &d.Notes, &d.ClosedBy, &d.ClosedByName, &d.ClosedAt)
	if err != nil {
		return DayEnd{}, fmt.Errorf("scan day end: %w", notFound(err))
	}
	return d, nil
}
