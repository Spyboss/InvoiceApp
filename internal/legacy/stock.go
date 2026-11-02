package legacy

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spyboss/invoiceapp/internal/money"
)

// Stock statuses a sheet may carry. A unit that has left the yard is not on
// the sheet: it left with a sale, and the sale import is what puts it on the
// record.
const (
	StockAvailable = "AVAILABLE"
	StockReserved  = "RESERVED"
)

// StockColumns is the header the stock sheet must carry, in the order the
// template hands out. It is exported so the screen can offer a blank template
// without spelling the list out a second time.
var StockColumns = []string{
	"stock_no", "make", "model", "vehicle_type", "variant", "chassis_no",
	"engine_no", "colour", "yom", "cost", "price", "received_on", "status", "notes",
}

// Unit is one row of the stock sheet: a vehicle standing in the yard, or one
// held for a customer.
type Unit struct {
	Line int

	StockNo   string
	Make      string
	Model     string
	Type      string
	Variant   string
	ChassisNo string
	EngineNo  string
	Colour    string
	YOM       *int32

	CostCents  int64
	PriceCents int64

	// ReceivedOn is the day the unit arrived, which is what the ageing report
	// counts from. A blank cell leaves it zero and the importer asks the
	// business date.
	ReceivedOn time.Time
	Status     string
	Notes      string
}

// StockTotals is what the sheet says the yard is worth. The two numbers are
// kept apart because they answer different questions, and neither is a gross.
type StockTotals struct {
	Units      int   `json:"units"`
	CostCents  int64 `json:"cost_cents"`
	PriceCents int64 `json:"price_cents"`
}

// Add folds one unit into the totals.
func (t *StockTotals) Add(u Unit) {
	t.Units++
	t.CostCents += u.CostCents
	t.PriceCents += u.PriceCents
}

// StockFile is a parsed stock sheet.
type StockFile struct {
	Units    []Unit
	Problems []Problem
	Totals   StockTotals
}

// ParseStock reads the stock sheet the dealership keeps. Money here is typed
// by a person, so it is read strictly by the same parser the screens use:
// three decimal places is a typo, not a rounding.
func ParseStock(r io.Reader) (StockFile, error) {
	var file StockFile
	var headerChecked bool
	seen := make(map[string]int)
	problem := func(line int, format string, args ...any) {
		file.Problems = append(file.Problems, Problem{Line: line, Problem: fmt.Sprintf(format, args...)})
	}

	err := records(r, func(header map[string]int, row []string, line int) error {
		if !headerChecked {
			headerChecked = true
			if err := required(header, "chassis_no", "model", "price"); err != nil {
				return err
			}
		}

		unit := Unit{
			Line:      line,
			StockNo:   cell(row, header, "stock_no"),
			Make:      cell(row, header, "make"),
			Model:     cell(row, header, "model"),
			Type:      cell(row, header, "vehicle_type"),
			Variant:   cell(row, header, "variant"),
			ChassisNo: cell(row, header, "chassis_no"),
			EngineNo:  cell(row, header, "engine_no"),
			Colour:    cell(row, header, "colour"),
			Notes:     cell(row, header, "notes"),
			Status:    strings.ToUpper(cell(row, header, "status")),
		}

		if unit.ChassisNo == "" {
			problem(line, "the chassis number is blank, and it is what identifies the unit")
			return nil
		}
		key := strings.ToUpper(unit.ChassisNo)
		if first, dup := seen[key]; dup {
			problem(line, "chassis %s is already on line %d", unit.ChassisNo, first)
			return nil
		}
		seen[key] = line

		if unit.Model == "" {
			problem(line, "chassis %s has no model", unit.ChassisNo)
			return nil
		}

		for _, amount := range []struct {
			column string
			into   *int64
		}{
			{"cost", &unit.CostCents},
			{"price", &unit.PriceCents},
		} {
			raw := cell(row, header, amount.column)
			if raw == "" {
				continue
			}
			cents, err := money.Parse(raw)
			if err != nil {
				problem(line, "the %s is not an amount: %v", amount.column, err)
				return nil
			}
			*amount.into = cents
		}

		if raw := cell(row, header, "yom"); raw != "" {
			year, err := strconv.Atoi(raw)
			if err != nil || year < 1990 || year > 2100 {
				problem(line, "the year of manufacture is %q, which is not a year between 1990 and 2100", raw)
				return nil
			}
			y := int32(year)
			unit.YOM = &y
		}

		if raw := cell(row, header, "received_on"); raw != "" {
			day, err := legacyDate(raw)
			if err != nil {
				problem(line, "the received date is %q, which is not a date: %v", raw, err)
				return nil
			}
			unit.ReceivedOn = day
		}

		switch unit.Status {
		case "":
			unit.Status = StockAvailable
		case StockAvailable, StockReserved:
		default:
			problem(line, "the status is %q; the sheet carries %s or %s units, and a unit that has left comes in with its sale",
				unit.Status, StockAvailable, StockReserved)
			return nil
		}

		file.Units = append(file.Units, unit)
		file.Totals.Add(unit)
		return nil
	})
	if err != nil {
		return StockFile{}, err
	}
	return file, nil
}
