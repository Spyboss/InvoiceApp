package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Errors that a screen can explain to a person. Anything else is a bug and is
// logged, not shown.
var (
	ErrDuplicateNIC         = errors.New("a customer with this nic already exists")
	ErrDuplicateChassis     = errors.New("a vehicle with this chassis number already exists")
	ErrDuplicateStockNo     = errors.New("this stock number is already in use")
	ErrStockConflict        = errors.New("the stock ledger would go negative or count one unit twice")
	ErrBadTransition        = errors.New("the vehicle is not in a state that allows that change")
	ErrInvalidDocument      = errors.New("the document details are not valid")
	ErrVehicleUnavailable   = errors.New("the vehicle is not available for this document")
	ErrQuoteNotOpen         = errors.New("the quotation is not open")
	ErrInvalidPayment       = errors.New("the payment details are not valid")
	ErrPaymentOverBalance   = errors.New("the payment is more than the invoice still owes")
	ErrPaymentNotReversible = errors.New("the receipt cannot be reversed")
	ErrDayAlreadyClosed     = errors.New("this day is already closed")
	ErrInvalidDayEnd        = errors.New("the day end details are not valid")
	ErrInvalidSettings      = errors.New("the settings are not valid")
	ErrInvalidUser          = errors.New("the user details are not valid")
	ErrLastOwner            = errors.New("a dealership needs one active owner")
)

// constraintName returns the unique index that rejected the write, if any.
func constraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

// sqlState returns the postgres error code, or "" for a non-database error.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
