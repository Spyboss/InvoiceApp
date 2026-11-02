package legacy

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Kinds are the invoice_type values the old app wrote. It numbered cash and
// leasing sales in one SALES series, so the kind alone does not say which of
// the two a sale was: what is still owed does.
const (
	KindSales    = "SALES"
	KindProforma = "PROFORMA"
	KindAdvance  = "ADVANCE"
)

// Paper is one row of the old app's invoices.csv: one document exactly as it
// was issued, with the customer and vehicle details copied onto it.
type Paper struct {
	// Line is where the row sits in the file, so a refusal can point at it.
	Line int

	Kind string
	No   string
	Date time.Time

	CustomerName    string
	CustomerNIC     string
	CustomerAddress string
	DeliveryAddress string

	Model     string
	EngineNo  string
	ChassisNo string
	Colour    string

	PriceCents   int64
	DownCents    int64
	BalanceCents int64

	FinanceCompany string
	FinanceAddress string
	PaymentMethod  string
}

// IsCash says a SALES row was paid in full, which is how a cash invoice is
// told apart from a leasing one in a file that used one series for both. A
// leasing sale that left nothing owing reads the same way, and it is the same
// fact on the paper: the customer owes nothing.
func (p Paper) IsCash() bool {
	return p.Kind == KindSales && p.BalanceCents == 0
}

// Totals is what a file says in one line. It carries json names because the
// import and the reconciliation store it, and a stored total that is compared
// with a live one must be the same shape on both sides.
type Totals struct {
	Papers       int   `json:"papers"`
	GrossCents   int64 `json:"gross_cents"`
	DownCents    int64 `json:"down_cents"`
	BalanceCents int64 `json:"balance_cents"`
}

// Add folds one paper into the totals.
func (t *Totals) Add(p Paper) {
	t.Papers++
	t.GrossCents += p.PriceCents
	t.DownCents += p.DownCents
	t.BalanceCents += p.BalanceCents
}

// InvoiceFile is a parsed invoices.csv: the papers it could read, the rows it
// could not, and what the whole file claims.
type InvoiceFile struct {
	Papers   []Paper
	Problems []Problem
	Totals   Totals
}

// ParseInvoices reads the old app's invoices.csv. A row that cannot be read is
// a problem with its line number and is left out of Papers; a file that is not
// this file at all is an error.
func ParseInvoices(r io.Reader) (InvoiceFile, error) {
	var file InvoiceFile
	seen := make(map[string]int)
	var headerChecked bool
	problem := func(line int, format string, args ...any) {
		file.Problems = append(file.Problems, Problem{Line: line, Problem: fmt.Sprintf(format, args...)})
	}

	err := records(r, func(header map[string]int, row []string, line int) error {
		if !headerChecked {
			headerChecked = true
			if err := required(header, "invoice_type", "invoice_no", "date", "customer",
				"chassis", "price", "down", "balance"); err != nil {
				return err
			}
		}

		paper := Paper{
			Line:            line,
			Kind:            strings.ToUpper(cell(row, header, "invoice_type")),
			No:              cell(row, header, "invoice_no"),
			CustomerName:    cell(row, header, "customer"),
			CustomerNIC:     cell(row, header, "nic"),
			CustomerAddress: cell(row, header, "cust_addr"),
			DeliveryAddress: cell(row, header, "delivery"),
			Model:           cell(row, header, "model"),
			EngineNo:        cell(row, header, "engine"),
			ChassisNo:       cell(row, header, "chassis"),
			Colour:          cell(row, header, "color"),
			FinanceCompany:  cell(row, header, "finance_company"),
			FinanceAddress:  cell(row, header, "finance_address"),
			PaymentMethod:   cell(row, header, "payment_method"),
		}

		switch paper.Kind {
		case KindSales, KindProforma, KindAdvance:
		case "":
			problem(line, "the invoice type is blank")
			return nil
		default:
			problem(line, "the invoice type is %q; the file should say %s, %s or %s",
				paper.Kind, KindSales, KindProforma, KindAdvance)
			return nil
		}

		if paper.No == "" {
			problem(line, "the invoice number is blank")
			return nil
		}
		key := paper.Kind + "-" + strings.ToUpper(paper.No)
		if first, dup := seen[key]; dup {
			problem(line, "%s %s is already on line %d", paper.Kind, paper.No, first)
			return nil
		}
		seen[key] = line

		date, err := legacyDate(cell(row, header, "date"))
		if err != nil {
			problem(line, "the date is %q, which is not a date: %v", cell(row, header, "date"), err)
			return nil
		}
		paper.Date = date

		if paper.CustomerName == "" {
			problem(line, "%s %s has no customer name", paper.Kind, paper.No)
			return nil
		}
		if paper.ChassisNo == "" {
			problem(line, "%s %s has no chassis number, so the paper cannot say which unit it sold",
				paper.Kind, paper.No)
			return nil
		}

		for _, amount := range []struct {
			column string
			into   *int64
		}{
			{"price", &paper.PriceCents},
			{"down", &paper.DownCents},
			{"balance", &paper.BalanceCents},
		} {
			cents, err := legacyMoney(cell(row, header, amount.column))
			if err != nil {
				problem(line, "the %s is not an amount: %v", amount.column, err)
				return nil
			}
			*amount.into = cents
		}

		file.Papers = append(file.Papers, paper)
		file.Totals.Add(paper)
		return nil
	})
	if err != nil {
		return InvoiceFile{}, err
	}
	return file, nil
}

// legacyDate reads the date the old app wrote, and the two shapes a
// spreadsheet turns it into when somebody opens and saves the file. A written
// out day and month is refused rather than guessed, because guessing wrong
// moves a sale into another month.
func legacyDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, errors.New("it is blank")
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02", "02-01-2006", "2-1-2006"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d, nil
		}
	}
	return time.Time{}, errors.New("use 2026-10-30")
}

// legacyMoney reads a money cell the way the old app wrote it. It wrote the
// result of Python float arithmetic, so the text can carry noise in the last
// digits ("900000.0499999999", which meant 900000.05). The value is recovered
// to the nearest cent, which is what the paper said, and from then on it is
// cents and never a float again.
func legacyMoney(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	lower := strings.ToLower(s)
	for _, prefix := range []string{"rs.", "rs", "lkr"} {
		if strings.HasPrefix(lower, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			break
		}
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, nil
	}
	if strings.HasPrefix(s, "-") {
		return 0, errors.New("an amount cannot be negative")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q is not a number", raw)
	}
	if v >= 1e12 {
		return 0, errors.New("the amount is too large")
	}
	return int64(math.Round(v * 100)), nil
}
