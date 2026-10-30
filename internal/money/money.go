// Package money parses and formats rupee amounts held as integer cents.
// Nothing outside this package may do arithmetic on a float.
package money

import (
	"errors"
	"fmt"
	"strings"
)

// Parse turns what a person typed into cents. It accepts digits with optional
// thousands separators and at most two decimal places. More than two decimals
// is an error rather than a silent rounding, because rounding money quietly is
// how a ledger stops matching the cash drawer.
func Parse(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errors.New("enter an amount")
	}
	if strings.HasPrefix(s, "-") {
		return 0, errors.New("amount cannot be negative")
	}

	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")

	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if len(frac) > 2 {
		return 0, errors.New("use at most two decimal places")
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if !hasDot {
		frac = "00"
	}

	if !allDigits(whole) {
		return 0, errors.New("amount must be a number, like 125000 or 1,250,000.00")
	}
	if !allDigits(frac) {
		return 0, errors.New("amount must be a number, like 125000 or 1,250,000.00")
	}

	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if len(whole) > 12 {
		return 0, errors.New("amount is too large")
	}

	var rupees int64
	for _, r := range whole {
		rupees = rupees*10 + int64(r-'0')
	}
	var cents int64
	for _, r := range frac {
		cents = cents*10 + int64(r-'0')
	}
	return rupees*100 + cents, nil
}

// Format renders cents for display, e.g. 125000050 -> "1,250,000.50".
func Format(cents int64) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	rupees := cents / 100
	frac := cents % 100

	digits := fmt.Sprintf("%d", rupees)
	var grouped strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}

	out := fmt.Sprintf("%s.%02d", grouped.String(), frac)
	if negative {
		return "-" + out
	}
	return out
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
