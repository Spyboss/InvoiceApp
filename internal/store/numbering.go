package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Document types. The strings are stored in documents.doc_type and are part
// of the database contract, so they are not renamed casually.
const (
	DocQuotation  = "QUOTATION"
	DocSalesCash  = "SALES_CASH"
	DocSalesLease = "SALES_LEASING"
	DocProforma   = "PROFORMA"
	DocAdvance    = "ADVANCE"
)

// Number series. A series is the counter a paper is drawn from, and it is not
// always one document type: a cash sale and a leasing sale are different
// documents but one series on paper, because the customer reads "SALES
// INVOICE" either way and the app this replaces spent one SALES number space
// for both (D26).
//
// A receipt (RCP) and a day end (Z) are numbered paper of their own. They are
// not rows in documents (a payment has no customer snapshot and no vehicle)
// but they take their numbers from the same counters, so a clerk cannot meet
// two papers with the same number.
const (
	SeriesSales   = "SALES"
	SeriesPayment = "PAYMENT"
	SeriesDayEnd  = "DAY_END"
)

// Document statuses. A quotation is open until it is converted; everything
// else is issued once.
const (
	DocOpen      = "OPEN"
	DocIssued    = "ISSUED"
	DocConverted = "CONVERTED"
	DocVoid      = "VOID"
)

// ValidDocType reports whether the type is one this app issues as a document.
func ValidDocType(t string) bool {
	switch t {
	case DocQuotation, DocSalesCash, DocSalesLease, DocProforma, DocAdvance:
		return true
	}
	return false
}

// SeriesFor maps a document type to the series that numbers it. The two sales
// types share one counter; everything else is numbered from its own.
func SeriesFor(docType string) string {
	switch docType {
	case DocSalesCash, DocSalesLease:
		return SeriesSales
	}
	return docType
}

// ValidSeries reports whether a number series exists. The two sales document
// types are deliberately absent: they are numbered from SeriesSales, so a
// caller that forgets SeriesFor is refused here rather than quietly opening a
// second sales counter beside the real one.
func ValidSeries(s string) bool {
	switch s {
	case DocQuotation, DocProforma, DocAdvance, SeriesSales, SeriesPayment, SeriesDayEnd:
		return true
	}
	return false
}

// serialMonths is the MMM segment of an invoice serial, three upper case
// letters as the Inland Revenue Department prescribes.
var serialMonths = [...]string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN",
	"JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}

// FormatSerial renders a number the way a tax invoice serial has to read in
// Sri Lanka: YYMMM_QQQQ_XXXXX, no spaces, at most 40 characters, carrying the
// year and month of the paper and a four character code for the counter it
// came from. 26NOV_TANG_00041 is the forty-first paper of 2026 from the
// Tangalle counter, dated in November.
//
// The counter runs per year, so a backdated paper can carry an earlier month
// than the one before it while the digits still only go up. A serial is
// unique, continuous and never reused, which is what the rule is for.
func FormatSerial(date time.Time, branch string, n int64) string {
	return fmt.Sprintf("%02d%s_%s_%05d", date.Year()%100, serialMonths[date.Month()-1], branch, n)
}

// serialTail returns the counter a stored number was drawn from. Numbers this
// app prints end in the counter ("26NOV_TANG_00041" gives 41) and the ones the
// old app printed are the counter on its own ("0041" gives 41), so the
// trailing run of digits is the only part worth comparing. A number that does
// not end in digits cannot be compared and is skipped, the same way the import
// leaves it on the paper untouched.
func serialTail(no string) (int64, bool) {
	s := strings.TrimSpace(no)
	if i := strings.LastIndexByte(s, '_'); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// branchCode reduces a tenant slug to the four characters a serial carries as
// its branch segment. One dealership runs one counter, so this is not a per
// paper choice, and it lives beside the slug rather than in the settings
// screen because the slug is fixed once when the tenant is created (D18). A
// slug that reduces to fewer than four characters is padded, so the segment is
// always the width the format asks for.
func branchCode(slug string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(slug) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == 4 {
				break
			}
		}
	}
	code := strings.ToUpper(b.String())
	for len(code) < 4 {
		code += "0"
	}
	return code
}

// AllocateNo bumps the series for (tenant, series, year) and returns the next
// number, in the tenant's serial format. The upsert takes a row lock, so two
// clerks issuing at the same moment get consecutive numbers, never the same
// one. It must run inside the same transaction as the paper it numbers: if
// that transaction rolls back, the counter rolls back with it and no gap is
// left.
func (s *Store) AllocateNo(ctx context.Context, tenantID uuid.UUID, series string, date time.Time) (string, error) {
	if !ValidSeries(series) {
		return "", ErrInvalidDocument
	}
	year := date.Year()
	if year < 2000 || year > 2100 {
		return "", ErrInvalidDocument
	}

	// The slug is read in the same statement, not before it: one round trip,
	// and the branch segment cannot come from a different tenant than the
	// counter that was just bumped.
	const q = `
		with branch as (
			select slug from tenants where id = $1
		), bumped as (
			insert into document_series (tenant_id, doc_type, year, last_value)
			values ($1, $2, $3, 1)
			on conflict (tenant_id, doc_type, year) do update
			set last_value = document_series.last_value + 1, updated_at = now()
			returning last_value
		)
		select branch.slug, bumped.last_value from bumped, branch`

	var (
		slug string
		n    int64
	)
	if err := s.db.QueryRow(ctx, q, tenantID, series, year).Scan(&slug, &n); err != nil {
		return "", fmt.Errorf("allocate number: %w", err)
	}
	return FormatSerial(date, branchCode(slug), n), nil
}
