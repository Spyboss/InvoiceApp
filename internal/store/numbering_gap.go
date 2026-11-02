package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// ScanNumberingGaps compares each series' counter with the numbers that were
// really spent, for the year given. It is read only, so it can run inside or
// outside a transaction; CloseDay calls it once a day, which is the cadence C2
// asks for. Must run inside InTenant.
func (s *Store) ScanNumberingGaps(ctx context.Context, tenantID uuid.UUID, year int) ([]NumberingGap, error) {
	if year < 2000 || year > 2100 {
		return nil, ErrInvalidDocument
	}

	const q = `
		select doc_type, last_value
		from document_series
		where tenant_id = $1 and year = $2 and last_value > 0
		order by doc_type`

	rows, err := s.db.Query(ctx, q, tenantID, year)
	if err != nil {
		return nil, fmt.Errorf("scan numbering gaps: %w", err)
	}
	type counter struct {
		series string
		last   int64
	}
	var counters []counter
	for rows.Next() {
		var c counter
		if err := rows.Scan(&c.series, &c.last); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan numbering gaps: %w", err)
		}
		counters = append(counters, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("scan numbering gaps: %w", err)
	}
	rows.Close()

	// The numbers are read as a set and walked, rather than counted: a count
	// answers "how many", but the owner needs "which one", and the two sales
	// series share one space on paper (see heldNumbers), so a count of one
	// series alone would report a false gap for every paper the other holds.
	var gaps []NumberingGap
	for _, c := range counters {
		held, err := s.heldNumbers(ctx, tenantID, c.series, year)
		if err != nil {
			return nil, err
		}

		var (
			issued       int64
			firstMissing int64
		)
		for n := int64(1); n <= c.last; n++ {
			if held[n] {
				issued++
				continue
			}
			if firstMissing == 0 {
				firstMissing = n
			}
		}
		if firstMissing == 0 {
			continue
		}
		gaps = append(gaps, NumberingGap{
			Series:       c.series,
			Year:         year,
			Expected:     c.last,
			Issued:       issued,
			FirstMissing: firstMissing,
			Missing:      c.last - issued,
		})
	}
	return gaps, nil
}

// heldNumbers reads the numbers one series has spent, as a set. Which table
// holds them depends on the series: documents carry the document types,
// payments carry the receipts, day_ends carry the z reports.
//
// The two sales series are read together on purpose. The old app numbered cash
// and leasing sales from one SALES counter, and importing a year of paper brings
// those numbers in unchanged, so on paper both series spent from one space. A
// number held by either is a number somebody really has in hand, and reporting
// it as missing from the other series would be a false alarm on every sale.
func (s *Store) heldNumbers(ctx context.Context, tenantID uuid.UUID, series string, year int) (map[int64]bool, error) {
	var (
		q    string
		args []any
	)
	switch series {
	case SeriesPayment:
		q = `select receipt_no from payments where tenant_id = $1 and receipt_year = $2`
		args = []any{tenantID, year}
	case SeriesDayEnd:
		q = `select z_no from day_ends where tenant_id = $1 and z_year = $2`
		args = []any{tenantID, year}
	case DocSalesCash, DocSalesLease:
		q = `select doc_no from documents
			where tenant_id = $1 and doc_year = $2 and doc_type in ('SALES_CASH', 'SALES_LEASING')`
		args = []any{tenantID, year}
	default:
		q = `select doc_no from documents where tenant_id = $1 and doc_year = $2 and doc_type = $3`
		args = []any{tenantID, year, series}
	}

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("read issued numbers: %w", err)
	}
	defer rows.Close()

	held := make(map[int64]bool)
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return nil, fmt.Errorf("read issued numbers: %w", err)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(no), 10, 64)
		if err != nil || n <= 0 {
			// A number that is not a number is worth knowing about, but it
			// cannot be compared, so it is skipped rather than failing the
			// whole scan. The import leaves such a number on the paper too.
			continue
		}
		held[n] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read issued numbers: %w", err)
	}
	return held, nil
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
