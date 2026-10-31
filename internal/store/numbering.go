package store

import (
	"context"
	"fmt"

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

// Series that are numbered paper but not rows in documents: a payment receipt
// (RCP) pays an invoice and a day end (Z) closes the drawer. They take their
// numbers from the same counters as the documents, so a clerk cannot meet two
// papers with the same number.
const (
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

// ValidSeries reports whether a number series exists. Documents and the two
// numbered papers share the counter table, so this is the wider set.
func ValidSeries(s string) bool {
	return ValidDocType(s) || s == SeriesPayment || s == SeriesDayEnd
}

// AllocateNo bumps the series for (tenant, series, year) and returns the new
// number, zero padded to four digits. The upsert takes a row lock, so two
// clerks issuing at the same moment get 0041 and 0042, never the same number.
// It must run inside the same transaction as the paper it numbers: if that
// transaction rolls back, the counter rolls back with it and no gap is left.
func (s *Store) AllocateNo(ctx context.Context, tenantID uuid.UUID, series string, year int) (string, error) {
	if !ValidSeries(series) {
		return "", ErrInvalidDocument
	}
	if year < 2000 || year > 2100 {
		return "", ErrInvalidDocument
	}

	const q = `
		insert into document_series (tenant_id, doc_type, year, last_value)
		values ($1, $2, $3, 1)
		on conflict (tenant_id, doc_type, year) do update
		set last_value = document_series.last_value + 1, updated_at = now()
		returning last_value`

	var n int64
	if err := s.db.QueryRow(ctx, q, tenantID, series, year).Scan(&n); err != nil {
		return "", fmt.Errorf("allocate number: %w", err)
	}
	return fmt.Sprintf("%04d", n), nil
}
