package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Errors that a screen can explain to a person. Anything else is a bug and is
// logged, not shown.
var (
	ErrDuplicateNIC     = errors.New("a customer with this nic already exists")
	ErrDuplicateChassis = errors.New("a vehicle with this chassis number already exists")
	ErrDuplicateStockNo = errors.New("this stock number is already in use")
	ErrStockConflict    = errors.New("the stock ledger would go negative or count one unit twice")
	ErrBadTransition    = errors.New("the vehicle is not in a state that allows that change")
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
