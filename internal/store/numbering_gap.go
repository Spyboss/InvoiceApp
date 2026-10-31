package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NumberingGap is one missing number in one series: the counter says the
// series has reached N, but fewer than N papers exist for that type and year.
// A gap means a number was spent and no paper carries it, which is either a
// rolled back transaction that did not really roll back or a hand in the
// database. Neither is tolerated silently (C2).
type NumberingGap struct {
	Series   string
	Year     int
	Expected int64
	Issued   int64
	// FirstMissing is the first number nobody holds, and Missing counts them,
	// so one row describes the whole run rather than one row per number.
	FirstMissing int64
	Missing      int64
}

// ScanNumberingGaps compares each series' counter with the papers that series
// actually issued, for the year given. It is read only, so it can run inside
// or outside a transaction; CloseDay calls it once a day, which is the cadence
// C2 asks for. Must run inside InTenant.
func (s *Store) ScanNumberingGaps(ctx context.Context, tenantID uuid.UUID, year int) ([]NumberingGap, error) {
	if year < 2000 || year > 2100 {
		return nil, ErrInvalidDocument
	}

	// Each series takes its numbers from a different table: documents carry
	// the document types, payments carry the receipts, day_ends carry the z
	// reports. Counting them together is what makes the comparison honest —
	// counting only documents would report a false gap for every receipt and
	// every z ever closed. The rows are drained before anything else queries
	// the connection, because pgx will not interleave two result sets.
	const q = `
		select ds.doc_type, ds.year, ds.last_value,
			coalesce(issued.n, 0)
		from document_series ds
		left join (
			select doc_type as series, doc_year as year, count(*) as n
			from documents
			where tenant_id = $1 and doc_year = $2
			group by doc_type, doc_year
			union all
			select 'PAYMENT' as series, receipt_year as year, count(*) as n
			from payments
			where tenant_id = $1 and receipt_year = $2
			group by receipt_year
			union all
			select 'DAY_END' as series, z_year as year, count(*) as n
			from day_ends
			where tenant_id = $1 and z_year = $2
			group by z_year
		) issued on issued.series = ds.doc_type and issued.year = ds.year
		where ds.tenant_id = $1 and ds.year = $2 and ds.last_value > 0
		order by ds.doc_type`

	rows, err := s.db.Query(ctx, q, tenantID, year)
	if err != nil {
		return nil, fmt.Errorf("scan numbering gaps: %w", err)
	}
	type candidate struct {
		series  string
		year    int
		last    int64
		issuedN int64
	}
	var behind []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.series, &c.year, &c.last, &c.issuedN); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan numbering gaps: %w", err)
		}
		if c.issuedN < c.last {
			behind = append(behind, c)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("scan numbering gaps: %w", err)
	}
	rows.Close()

	// The counter is ahead of the papers. Rather than list every missing
	// number, find the first one nobody holds and report the run, which is
	// what a person needs to go and look for.
	var gaps []NumberingGap
	for _, c := range behind {
		first, err := s.firstMissingNo(ctx, tenantID, c.series, c.year, c.last)
		if err != nil {
			return nil, err
		}
		gaps = append(gaps, NumberingGap{
			Series:       c.series,
			Year:         c.year,
			Expected:     c.last,
			Issued:       c.issuedN,
			FirstMissing: first,
			Missing:      c.last - c.issuedN,
		})
	}
	return gaps, nil
}

// firstMissingNo walks the numbers a series has spent and returns the first
// one no paper carries. It reads the issued numbers as a set and steps through
// 1..last, so it finds the head of a run of gaps rather than only that some
// exist. A series spends numbers 1..last with none missing, so the walk ends
// at last+1 and the caller never sees it. Which table holds the numbers
// depends on the series, the same split the scan counts from.
func (s *Store) firstMissingNo(ctx context.Context, tenantID uuid.UUID, series string, year int, last int64) (int64, error) {
	const docQ = `
		select doc_no
		from documents
		where tenant_id = $1 and doc_type = $2 and doc_year = $3`
	const payQ = `
		select receipt_no
		from payments
		where tenant_id = $1 and receipt_year = $2`
	const zQ = `
		select z_no
		from day_ends
		where tenant_id = $1 and z_year = $2`

	var (
		q    string
		args []any
	)
	switch series {
	case SeriesPayment:
		q, args = payQ, []any{tenantID, year}
	case SeriesDayEnd:
		q, args = zQ, []any{tenantID, year}
	default:
		q, args = docQ, []any{tenantID, series, year}
	}

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("read issued numbers: %w", err)
	}
	defer rows.Close()

	held := make(map[int64]bool, last)
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return 0, fmt.Errorf("read issued numbers: %w", err)
		}
		var n int64
		if _, err := fmt.Sscanf(no, "%d", &n); err != nil {
			// A number that does not parse is itself worth knowing about,
			// but it cannot be compared, so it is skipped rather than
			// failing the whole scan.
			continue
		}
		held[n] = true
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read issued numbers: %w", err)
	}

	for n := int64(1); n <= last; n++ {
		if !held[n] {
			return n, nil
		}
	}
	return last + 1, nil
}

// RecordNumberingGapAlerts turns each gap into an alert, in the caller's
// transaction. It is called by the day end, so the z report and the alert land
// together or not at all. Must run inside InTenant.
func (s *Store) RecordNumberingGapAlerts(ctx context.Context, tenantID uuid.UUID, year int) (int, error) {
	gaps, err := s.ScanNumberingGaps(ctx, tenantID, year)
	if err != nil {
		return 0, err
	}
	for _, g := range gaps {
		message := fmt.Sprintf(
			"The %s series for %d has reached %04d but only %d papers exist. Number %04d is missing, and %d numbers in total.",
			SeriesLabel(g.Series), g.Year, g.Expected, g.Issued, g.FirstMissing, g.Missing)
		if _, err := s.RecordAlert(ctx, tenantID, AlertParams{
			Kind:     AlertNumberingGap,
			Message:  message,
			Severity: SeverityCritical,
			Entity:   fmt.Sprintf("%s-%04d", g.Series, g.FirstMissing),
		}); err != nil {
			return 0, err
		}
	}
	return len(gaps), nil
}

// SeriesLabel spells a series code out for a sentence. The code itself stays
// in the row and in the reports; a person reading an alert sees the name.
func SeriesLabel(series string) string {
	switch series {
	case SeriesPayment:
		return "receipts"
	case SeriesDayEnd:
		return "z reports"
	case DocQuotation:
		return "quotations"
	case DocSalesCash:
		return "cash invoices"
	case DocSalesLease:
		return "leasing invoices"
	case DocProforma:
		return "proformas"
	case DocAdvance:
		return "advance receipts"
	}
	return series
}

// alertYear is the year the day end scans for gaps: the year of the business
// date being closed, which is where that day's numbers were spent.
func alertYear(window DayWindow) int {
	if window.Date.IsZero() {
		return time.Now().Year()
	}
	return window.Date.Year()
}
