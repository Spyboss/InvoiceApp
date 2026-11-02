package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/legacy"
	"github.com/spyboss/invoiceapp/internal/money"
)

// The two kinds of file this package can take in, stored in import_runs.kind.
// The kind decides what the row's two total blobs mean, so it is never guessed
// from the file name.
const (
	ImportInvoices = "INVOICES"
	ImportStock    = "STOCK"
)

// ImportProblem is one reason a file was not imported: the line it is on (zero
// when it is about the file itself) and a sentence somebody can act on.
type ImportProblem struct {
	Line    int
	Problem string
}

// ImportRefusal is a file that was not imported at all. Nothing half way is
// ever kept: a file with one bad row is corrected and uploaded again, because a
// silently dropped row is a sale that never reaches the book.
type ImportRefusal struct {
	Problems []ImportProblem
}

func (r *ImportRefusal) Error() string {
	const shown = 5
	var b strings.Builder
	b.WriteString("the file cannot be imported")
	for i, p := range r.Problems {
		if i == shown {
			fmt.Fprintf(&b, "; and %d more", len(r.Problems)-shown)
			break
		}
		if p.Line > 0 {
			fmt.Fprintf(&b, "; line %d: %s", p.Line, p.Problem)
			continue
		}
		fmt.Fprintf(&b, "; %s", p.Problem)
	}
	return b.String()
}

// Is lets a handler answer a refusal with errors.Is instead of a type switch.
func (r *ImportRefusal) Is(target error) bool { return target == ErrImportRefused }

// InvoiceImport is one upload of the old app's invoices.csv, already parsed.
type InvoiceImport struct {
	Filename string
	SHA256   string
	// DefaultMake is what a unit gets when the file does not name a make. The
	// invoices file carries a model and no make, and a vehicle row needs one;
	// the person importing says which, rather than this package guessing.
	DefaultMake string
	File        legacy.InvoiceFile
}

// InvoiceImportResult is what one run did and what both sides agreed on.
type InvoiceImportResult struct {
	RunID    uuid.UUID
	Imported int
	Skipped  int
	Totals   legacy.Totals
}

// ImportInvoices writes the papers in one invoices.csv into the book as they
// already stand: same number, same date, same customer, same unit. The run is
// all or nothing, and running it twice over the same file changes nothing the
// second time. Must run inside InTenant, in the caller's transaction, so the
// audit row and the import land together.
func (s *Store) ImportInvoices(ctx context.Context, tenantID, actorID uuid.UUID, in InvoiceImport) (InvoiceImportResult, error) {
	var zero InvoiceImportResult
	if tenantID == uuid.Nil || actorID == uuid.Nil {
		return zero, errors.New("store: an import needs a tenant and an actor")
	}
	if problems := invoiceProblems(in); len(problems) > 0 {
		return zero, &ImportRefusal{Problems: problems}
	}
	if len(in.File.Papers) == 0 {
		return zero, &ImportRefusal{Problems: []ImportProblem{{Problem: "the file has no rows to import"}}}
	}

	runID, err := uuid.NewV7()
	if err != nil {
		return zero, fmt.Errorf("import id: %w", err)
	}
	res := InvoiceImportResult{RunID: runID, Totals: in.File.Totals}

	// First pass: read only. Everything the app already holds under these
	// numbers is checked before one row is written, so a file that disagrees
	// with the book leaves no trace at all.
	held := make(map[int]*Document, len(in.File.Papers))
	var problems []ImportProblem
	for i, paper := range in.File.Papers {
		doc, err := s.documentByNo(ctx, tenantID, paperDocType(paper), paper.Date.Year(), paper.No)
		if err != nil {
			return zero, err
		}
		if doc == nil {
			continue
		}
		if mismatch := documentMismatch(*doc, paper); mismatch != "" {
			problems = append(problems, ImportProblem{Line: paper.Line, Problem: mismatch})
			continue
		}
		held[i] = doc
	}
	if len(problems) > 0 {
		return zero, &ImportRefusal{Problems: problems}
	}

	// Second pass: write.
	for i, paper := range in.File.Papers {
		if held[i] != nil {
			res.Skipped++
			continue
		}
		docType := paperDocType(paper)

		customer, err := s.importCustomer(ctx, tenantID, actorID, paper)
		if err != nil {
			return zero, err
		}
		vehicle, err := s.importVehicle(ctx, tenantID, actorID, paper, in.DefaultMake)
		if err != nil {
			return zero, err
		}
		if err := s.importVehicleEffect(ctx, tenantID, actorID, docType, paper, vehicle); err != nil {
			return zero, err
		}
		if err := s.insertImportedDocument(ctx, tenantID, actorID, runID, docType, paper, customer, vehicle); err != nil {
			return zero, err
		}
		res.Imported++
	}

	// The file's own numbers are compared with what the database now holds.
	// The import_runs_verified check repeats this comparison in the database,
	// so a run that does not balance cannot be written down at all.
	stored, err := s.importedInvoiceTotals(ctx, tenantID, in.File.Papers)
	if err != nil {
		return zero, err
	}
	if stored != in.File.Totals {
		return zero, fmt.Errorf("import invoices: the app holds %d papers worth %s, the file says %d worth %s",
			stored.Papers, money.Format(stored.GrossCents), in.File.Totals.Papers, money.Format(in.File.Totals.GrossCents))
	}
	res.Totals = stored

	if err := s.insertImportRun(ctx, tenantID, actorID, runID, ImportInvoices,
		in.Filename, in.SHA256, in.File.Totals, stored, res.Imported, res.Skipped); err != nil {
		return zero, err
	}
	if err := s.raiseImportedSeries(ctx, tenantID, in.File.Papers); err != nil {
		return zero, err
	}
	return res, nil
}

// StockImport is one upload of the yard sheet, already parsed.
type StockImport struct {
	Filename    string
	SHA256      string
	DefaultMake string
	// ReceivedOn dates a unit the sheet leaves blank. A row with no arrival
	// date still has to age from something, and the day of the import is the
	// only honest answer.
	ReceivedOn time.Time
	File       legacy.StockFile
}

// StockImportResult is what one run did and what both sides agreed on.
type StockImportResult struct {
	RunID    uuid.UUID
	Imported int
	Skipped  int
	Totals   legacy.StockTotals
}

// ImportStock brings the units standing in the yard onto the ledger. A unit the
// app already has is left alone and its money checked; a unit it does not have
// arrives exactly as an intake would record it. Must run inside InTenant.
func (s *Store) ImportStock(ctx context.Context, tenantID, actorID uuid.UUID, in StockImport) (StockImportResult, error) {
	var zero StockImportResult
	if tenantID == uuid.Nil || actorID == uuid.Nil {
		return zero, errors.New("store: an import needs a tenant and an actor")
	}
	if in.ReceivedOn.IsZero() {
		return zero, errors.New("store: a stock import needs the day to date units from")
	}
	if problems := stockProblems(in); len(problems) > 0 {
		return zero, &ImportRefusal{Problems: problems}
	}
	if len(in.File.Units) == 0 {
		return zero, &ImportRefusal{Problems: []ImportProblem{{Problem: "the sheet has no rows to import"}}}
	}

	runID, err := uuid.NewV7()
	if err != nil {
		return zero, fmt.Errorf("import id: %w", err)
	}
	res := StockImportResult{RunID: runID, Totals: in.File.Totals}

	for _, unit := range in.File.Units {
		found, err := s.vehicleByChassis(ctx, tenantID, unit.ChassisNo)
		if err != nil {
			return zero, err
		}
		if found != nil {
			if problem := stockMismatch(*found, unit); problem != "" {
				return zero, &ImportRefusal{Problems: []ImportProblem{{Line: unit.Line, Problem: problem}}}
			}
			if unit.Status == legacy.StockReserved && found.Status == StatusAvailable {
				if err := s.markVehicle(ctx, tenantID, found.ID, StatusReserved); err != nil {
					return zero, err
				}
			}
			res.Skipped++
			continue
		}

		received := unit.ReceivedOn
		if received.IsZero() {
			received = in.ReceivedOn
		}
		make := normaliseMake(unit.Make, in.DefaultMake)
		vehicle, err := s.CreateVehicle(ctx, tenantID, actorID, VehicleParams{
			StockNo:    unit.StockNo,
			Make:       make,
			Model:      unit.Model,
			Type:       unit.Type,
			Variant:    unit.Variant,
			ChassisNo:  unit.ChassisNo,
			EngineNo:   unit.EngineNo,
			Colour:     unit.Colour,
			YOM:        unit.YOM,
			CostCents:  unit.CostCents,
			PriceCents: unit.PriceCents,
			ReceivedAt: received,
			Notes:      unit.Notes,
		})
		if err != nil {
			return zero, err
		}
		if unit.Status == legacy.StockReserved {
			if err := s.markVehicle(ctx, tenantID, vehicle.ID, StatusReserved); err != nil {
				return zero, err
			}
		}
		res.Imported++
	}

	chassis := make([]string, len(in.File.Units))
	for i, unit := range in.File.Units {
		chassis[i] = strings.ToUpper(strings.TrimSpace(unit.ChassisNo))
	}
	const q = `
		select count(*), coalesce(sum(cost_cents), 0), coalesce(sum(price_cents), 0)
		from vehicles
		where tenant_id = $1 and deleted_at is null and upper(chassis_no) = any($2::text[])`
	var stored legacy.StockTotals
	if err := s.db.QueryRow(ctx, q, tenantID, chassis).Scan(&stored.Units, &stored.CostCents, &stored.PriceCents); err != nil {
		return zero, fmt.Errorf("count imported units: %w", err)
	}
	if stored != in.File.Totals {
		return zero, fmt.Errorf("import stock: the app holds %d units, the sheet says %d",
			stored.Units, in.File.Totals.Units)
	}
	res.Totals = stored

	if err := s.insertImportRun(ctx, tenantID, actorID, runID, ImportStock,
		in.Filename, in.SHA256, in.File.Totals, stored, res.Imported, res.Skipped); err != nil {
		return zero, err
	}
	return res, nil
}

// paperDocType files one legacy row under the document type this app numbers
// and reports it as. The old app put cash and leasing sales in one SALES
// series, so what is still owed is what tells them apart.
func paperDocType(p legacy.Paper) string {
	switch p.Kind {
	case legacy.KindSales:
		if p.IsCash() {
			return DocSalesCash
		}
		return DocSalesLease
	case legacy.KindProforma:
		return DocProforma
	case legacy.KindAdvance:
		return DocAdvance
	}
	return ""
}

// invoiceProblems turns everything wrong with one file into sentences, each on
// the line it belongs to. The parser's own problems come first, because a row
// it could not read never reaches the checks below.
func invoiceProblems(in InvoiceImport) []ImportProblem {
	var problems []ImportProblem
	add := func(line int, format string, args ...any) {
		problems = append(problems, ImportProblem{Line: line, Problem: fmt.Sprintf(format, args...)})
	}

	if len(in.SHA256) != 64 {
		add(0, "the file contents could not be identified, so the import cannot be proven later")
	}
	if tooLong(in.Filename, 200) {
		add(0, "the file name is longer than 200 characters")
	}
	if strings.TrimSpace(in.DefaultMake) == "" {
		add(0, "say which make to give a unit the invoices file does not name")
	} else if tooLong(in.DefaultMake, 80) {
		add(0, "the make is longer than 80 characters")
	}

	for _, p := range in.File.Problems {
		add(p.Line, "%s", p.Problem)
	}

	for _, p := range in.File.Papers {
		switch {
		case p.PriceCents <= 0:
			add(p.Line, "%s %s has a price of zero", p.Kind, p.No)
		case p.DownCents+p.BalanceCents != p.PriceCents:
			add(p.Line, "%s %s does not add up: the price is %s, but the down payment and the balance come to %s",
				p.Kind, p.No, money.Format(p.PriceCents), money.Format(p.DownCents+p.BalanceCents))
		}

		switch paperDocType(p) {
		case DocSalesLease, DocProforma:
			if p.DownCents >= p.PriceCents {
				add(p.Line, "%s %s leaves nothing for the finance company to pay, so it is not a leasing sale",
					p.Kind, p.No)
			}
			if strings.TrimSpace(p.FinanceCompany) == "" {
				add(p.Line, "%s %s has no finance company, and the leasing paper cannot be printed without one",
					p.Kind, p.No)
			}
		case DocAdvance:
			if p.DownCents <= 0 {
				add(p.Line, "the advance receipt %s records no deposit", p.No)
			}
			if p.DownCents >= p.PriceCents {
				add(p.Line, "the advance receipt %s records the whole price as a deposit, which is a sale", p.No)
			}
		}

		for _, limit := range []struct {
			what  string
			value string
			max   int
		}{
			{"the invoice number", p.No, 32},
			{"the customer name", p.CustomerName, 120},
			{"the nic", p.CustomerNIC, 20},
			{"the customer address", p.CustomerAddress, 500},
			{"the delivery address", p.DeliveryAddress, 500},
			{"the model", p.Model, 80},
			{"the engine number", p.EngineNo, 40},
			{"the chassis number", p.ChassisNo, 40},
			{"the colour", p.Colour, 40},
			{"the finance company", p.FinanceCompany, 120},
			{"the finance address", p.FinanceAddress, 500},
		} {
			if tooLong(limit.value, limit.max) {
				add(p.Line, "%s on %s %s is longer than %d characters", limit.what, p.Kind, p.No, limit.max)
			}
		}
		if len(strings.TrimSpace(p.ChassisNo)) < 5 {
			add(p.Line, "the chassis number on %s %s is shorter than 5 characters", p.Kind, p.No)
		}
		if year := p.Date.Year(); year < 2000 || year > 2100 {
			add(p.Line, "%s %s is dated %d, which is outside the years this app keeps",
				p.Kind, p.No, year)
		}
	}
	return problems
}

// stockProblems is the same for the yard sheet: lengths the vehicle row
// enforces, and money it refuses.
func stockProblems(in StockImport) []ImportProblem {
	var problems []ImportProblem
	add := func(line int, format string, args ...any) {
		problems = append(problems, ImportProblem{Line: line, Problem: fmt.Sprintf(format, args...)})
	}

	if len(in.SHA256) != 64 {
		add(0, "the file contents could not be identified, so the import cannot be proven later")
	}
	if tooLong(in.Filename, 200) {
		add(0, "the file name is longer than 200 characters")
	}
	if strings.TrimSpace(in.DefaultMake) == "" {
		// The sheet may name a make row by row, so a default is only owed for
		// the rows that leave it blank. Asking for one anyway would be a form
		// field that changes nothing.
		for _, unit := range in.File.Units {
			if strings.TrimSpace(unit.Make) == "" {
				add(0, "say which make to give the units the sheet does not name")
				break
			}
		}
	} else if tooLong(in.DefaultMake, 80) {
		add(0, "the make is longer than 80 characters")
	}

	for _, p := range in.File.Problems {
		add(p.Line, "%s", p.Problem)
	}
	for _, unit := range in.File.Units {
		for _, limit := range []struct {
			what  string
			value string
			max   int
		}{
			{"the stock number", unit.StockNo, 40},
			{"the model", unit.Model, 80},
			{"the unit type", unit.Type, 40},
			{"the variant", unit.Variant, 40},
			{"the engine number", unit.EngineNo, 40},
			{"the colour", unit.Colour, 40},
			{"the notes", unit.Notes, 2000},
		} {
			if tooLong(limit.value, limit.max) {
				add(unit.Line, "%s on unit %s is longer than %d characters", limit.what, unit.ChassisNo, limit.max)
			}
		}
		if unit.CostCents < 0 || unit.PriceCents < 0 {
			add(unit.Line, "unit %s has a negative amount", unit.ChassisNo)
		}
	}
	return problems
}

// normaliseMake is the make a unit gets: its own when the file names one, the
// importer's answer when it does not.
func normaliseMake(own, fallback string) string {
	if make := strings.TrimSpace(own); make != "" {
		return make
	}
	return strings.TrimSpace(fallback)
}

func tooLong(s string, max int) bool { return len([]rune(s)) > max }

// documentByNo finds the paper the app already holds under the number the old
// app gave it, or nil.
func (s *Store) documentByNo(ctx context.Context, tenantID uuid.UUID, docType string, year int, no string) (*Document, error) {
	q := `select ` + documentColumns + ` from documents
		where tenant_id = $1 and doc_type = $2 and doc_year = $3 and doc_no = $4`
	doc, err := scanDocument(s.db.QueryRow(ctx, q, tenantID, docType, year, no))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// documentMismatch says how a paper the app already holds differs from the row
// in the file, or "" when they agree. A disagreement is not repaired quietly:
// the file is wrong, or the wrong file was uploaded, and a person decides
// which.
func documentMismatch(d Document, p legacy.Paper) string {
	label := p.Kind + " " + p.No
	switch {
	case d.PriceCents != p.PriceCents:
		return fmt.Sprintf("%s is already in the app with a price of %s, but the file says %s",
			label, money.Format(d.PriceCents), money.Format(p.PriceCents))
	case d.DownCents != p.DownCents:
		return fmt.Sprintf("%s is already in the app with a down payment of %s, but the file says %s",
			label, money.Format(d.DownCents), money.Format(p.DownCents))
	case d.BalanceCents != p.BalanceCents:
		return fmt.Sprintf("%s is already in the app with a balance of %s, but the file says %s",
			label, money.Format(d.BalanceCents), money.Format(p.BalanceCents))
	case !strings.EqualFold(strings.TrimSpace(d.VehicleChassisNo), strings.TrimSpace(p.ChassisNo)):
		return fmt.Sprintf("%s is already in the app for chassis %s, but the file says %s",
			label, d.VehicleChassisNo, p.ChassisNo)
	case !strings.EqualFold(strings.TrimSpace(d.CustomerName), strings.TrimSpace(p.CustomerName)):
		return fmt.Sprintf("%s is already in the app for %s, but the file says %s",
			label, d.CustomerName, p.CustomerName)
	}
	return ""
}

// importCustomer reuses the customer the app already knows for this nic, or for
// this name when the file carries no nic, and otherwise creates one. Five years
// of paper must not become five hundred copies of the same person.
func (s *Store) importCustomer(ctx context.Context, tenantID, actorID uuid.UUID, p legacy.Paper) (Customer, error) {
	nic := strings.TrimSpace(p.CustomerNIC)
	if nic != "" {
		var (
			id      uuid.UUID
			deleted *time.Time
		)
		err := s.db.QueryRow(ctx,
			`select id, deleted_at from customers where tenant_id = $1 and upper(nic) = upper($2)`,
			tenantID, nic).Scan(&id, &deleted)
		switch {
		case err == nil && deleted != nil:
			return Customer{}, &ImportRefusal{Problems: []ImportProblem{{Line: p.Line, Problem: fmt.Sprintf(
				"the customer with nic %s was deleted, and %s %s is for them; restore the customer first",
				nic, p.Kind, p.No)}}}
		case err == nil:
			return s.customerByID(ctx, tenantID, id)
		case !errors.Is(notFound(err), ErrNotFound):
			return Customer{}, fmt.Errorf("find customer by nic: %w", err)
		}
	}

	name := strings.TrimSpace(p.CustomerName)
	if name != "" {
		q := `select ` + customerColumns + ` from customers
			where tenant_id = $1 and deleted_at is null and lower(name) = lower($2)
			order by created_at limit 1`
		c, err := scanCustomer(s.db.QueryRow(ctx, q, tenantID, name))
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Customer{}, fmt.Errorf("find customer by name: %w", err)
		}
	}

	const insert = `
		insert into customers (tenant_id, name, nic, address, delivery_address, notes, created_by)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning ` + customerColumns
	c, err := scanCustomer(s.db.QueryRow(ctx, insert, tenantID, name, nic,
		strings.TrimSpace(p.CustomerAddress), strings.TrimSpace(p.DeliveryAddress),
		"imported with "+p.Kind+" "+p.No, actorID))
	if constraintName(err) == "customers_nic_idx" {
		return Customer{}, &ImportRefusal{Problems: []ImportProblem{{Line: p.Line, Problem: fmt.Sprintf(
			"another customer took nic %s while this file was importing; try again", nic)}}}
	}
	if err != nil {
		return Customer{}, fmt.Errorf("create customer: %w", err)
	}
	return c, nil
}

// customerByID reads one customer, deleted or not: the caller has already
// decided the row it found is the right one.
func (s *Store) customerByID(ctx context.Context, tenantID, id uuid.UUID) (Customer, error) {
	q := `select ` + customerColumns + ` from customers where tenant_id = $1 and id = $2`
	return scanCustomer(s.db.QueryRow(ctx, q, tenantID, id))
}

// vehicleByChassis finds the unit the app already has for this chassis, taking
// the row lock a sale would take, or nil.
func (s *Store) vehicleByChassis(ctx context.Context, tenantID uuid.UUID, chassis string) (*Vehicle, error) {
	q := `select ` + vehicleColumns + ` from vehicles
		where tenant_id = $1 and upper(chassis_no) = upper($2) and deleted_at is null for update`
	v, err := scanVehicle(s.db.QueryRow(ctx, q, tenantID, strings.TrimSpace(chassis)))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// importVehicle reuses the unit the file names and otherwise adds it, with the
// same RECEIVE movement an intake writes, so the yard count and the ledger
// agree from the first day of the imported book.
func (s *Store) importVehicle(ctx context.Context, tenantID, actorID uuid.UUID, p legacy.Paper, defaultMake string) (Vehicle, error) {
	found, err := s.vehicleByChassis(ctx, tenantID, p.ChassisNo)
	if err != nil {
		return Vehicle{}, err
	}
	if found != nil {
		return *found, nil
	}

	vehicle, err := s.CreateVehicle(ctx, tenantID, actorID, VehicleParams{
		Make:       normaliseMake("", defaultMake),
		Model:      p.Model,
		ChassisNo:  p.ChassisNo,
		EngineNo:   p.EngineNo,
		Colour:     p.Colour,
		PriceCents: p.PriceCents,
		ReceivedAt: p.Date,
		Notes:      "imported with " + p.Kind + " " + p.No,
	})
	if constraintName(err) == "vehicles_chassis_idx" {
		return Vehicle{}, &ImportRefusal{Problems: []ImportProblem{{Line: p.Line, Problem: fmt.Sprintf(
			"another unit took chassis %s while this file was importing; try again", p.ChassisNo)}}}
	}
	if err != nil {
		return Vehicle{}, err
	}
	return vehicle, nil
}

// importVehicleEffect moves the unit the way the paper says it moved: a sale
// takes it out of the yard, an advance holds it, and a proforma does not touch
// it. A unit that has already left cannot leave twice.
func (s *Store) importVehicleEffect(ctx context.Context, tenantID, actorID uuid.UUID, docType string, p legacy.Paper, v Vehicle) error {
	switch docType {
	case DocSalesCash, DocSalesLease:
		if v.Status != StatusAvailable && v.Status != StatusReserved {
			return &ImportRefusal{Problems: []ImportProblem{{Line: p.Line, Problem: fmt.Sprintf(
				"%s %s sells chassis %s, which the app already has as %s; a unit cannot be sold twice",
				p.Kind, p.No, v.ChassisNo, strings.ToLower(v.Status))}}}
		}
		if err := s.markVehicle(ctx, tenantID, v.ID, StatusSold); err != nil {
			return err
		}
		return s.insertMovement(ctx, movement{
			tenantID:  tenantID,
			vehicleID: v.ID,
			kind:      "SELL",
			delta:     -1,
			from:      v.Status,
			to:        StatusSold,
			note:      "sale " + p.No + " (imported)",
			actorID:   actorID,
		})
	case DocAdvance:
		if v.Status == StatusAvailable {
			return s.markVehicle(ctx, tenantID, v.ID, StatusReserved)
		}
		return nil
	}
	return nil
}

// insertImportedDocument writes a paper that already exists on paper. The
// number is the one the old app gave it, so it is never allocated here: the
// counter is lifted afterwards, above every imported number, and what is
// printed in the app stays what is printed on the paper.
func (s *Store) insertImportedDocument(ctx context.Context, tenantID, actorID, runID uuid.UUID, docType string, p legacy.Paper, customer Customer, v Vehicle) error {
	const q = `
		insert into documents (tenant_id, doc_type, doc_no, doc_year, doc_date, status,
			customer_id, vehicle_id, customer_name, customer_nic, customer_address, customer_phone,
			vehicle_make, vehicle_model, vehicle_type, vehicle_colour, vehicle_engine_no,
			vehicle_chassis_no, vehicle_yom, finance_company, finance_address,
			price_cents, down_cents, balance_cents, created_by, imported_at, import_run_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24, $25, now(), $26)`

	_, err := s.db.Exec(ctx, q,
		tenantID, docType, p.No, p.Date.Year(), p.Date, DocIssued,
		customer.ID, v.ID, strings.TrimSpace(p.CustomerName), strings.TrimSpace(p.CustomerNIC),
		strings.TrimSpace(p.CustomerAddress), customer.Phone,
		v.Make, strings.TrimSpace(p.Model), v.Type, strings.TrimSpace(p.Colour),
		strings.TrimSpace(p.EngineNo), strings.TrimSpace(p.ChassisNo), v.YOM,
		strings.TrimSpace(p.FinanceCompany), strings.TrimSpace(p.FinanceAddress),
		p.PriceCents, p.DownCents, p.BalanceCents, actorID, runID)
	if err != nil {
		return fmt.Errorf("import document %s %s: %w", p.Kind, p.No, err)
	}
	return nil
}

// importedInvoiceTotals counts the file's papers as the database now holds
// them. This is the number the import is accepted on, so it is read back from
// the rows rather than carried along in memory.
func (s *Store) importedInvoiceTotals(ctx context.Context, tenantID uuid.UUID, papers []legacy.Paper) (legacy.Totals, error) {
	types := make([]string, len(papers))
	years := make([]int32, len(papers))
	nos := make([]string, len(papers))
	for i, p := range papers {
		types[i] = paperDocType(p)
		years[i] = int32(p.Date.Year())
		nos[i] = p.No
	}

	const q = `
		select count(*), coalesce(sum(d.price_cents), 0), coalesce(sum(d.down_cents), 0),
			coalesce(sum(d.balance_cents), 0)
		from documents d
		join unnest($2::text[], $3::integer[], $4::text[]) as k (doc_type, doc_year, doc_no)
		  on d.doc_type = k.doc_type and d.doc_year = k.doc_year and d.doc_no = k.doc_no
		where d.tenant_id = $1`

	var t legacy.Totals
	if err := s.db.QueryRow(ctx, q, tenantID, types, years, nos).
		Scan(&t.Papers, &t.GrossCents, &t.DownCents, &t.BalanceCents); err != nil {
		return legacy.Totals{}, fmt.Errorf("count imported papers: %w", err)
	}
	return t, nil
}

// raiseImportedSeries lifts each series above the highest number the old app
// handed out, so the first paper this app issues does not reuse a number that
// is already on a customer's desk. It never lowers a counter.
//
// The two sales series are lifted together, to the higher of the two. They
// print the same thing on paper ("SALES INVOICE No. 0007"), and the old app
// spent one SALES number space for both, so lifting cash only to the highest
// cash number would hand out a number a leasing invoice already carries.
func (s *Store) raiseImportedSeries(ctx context.Context, tenantID uuid.UUID, papers []legacy.Paper) error {
	type seriesKey struct {
		docType string
		year    int
	}
	highest := make(map[seriesKey]int64, len(papers))
	for _, p := range papers {
		n, err := strconv.ParseInt(strings.TrimSpace(p.No), 10, 64)
		if err != nil || n <= 0 {
			// A number that is not a number cannot lift a counter. It stays on
			// the paper exactly as the old app wrote it, and the scan for gaps
			// skips it the same way.
			continue
		}
		k := seriesKey{docType: paperDocType(p), year: p.Date.Year()}
		if n > highest[k] {
			highest[k] = n
		}
	}
	// A pair shares one space on paper, so both are lifted to the higher of
	// the two, for every year the file has a sale in.
	byYear := make(map[int]int64)
	for k, n := range highest {
		if k.docType == DocSalesCash || k.docType == DocSalesLease {
			if n > byYear[k.year] {
				byYear[k.year] = n
			}
		}
	}
	lift := make(map[seriesKey]int64, len(highest)+len(byYear))
	for k, n := range highest {
		lift[k] = n
	}
	for year, n := range byYear {
		lift[seriesKey{docType: DocSalesCash, year: year}] = n
		lift[seriesKey{docType: DocSalesLease, year: year}] = n
	}

	const q = `
		insert into document_series (tenant_id, doc_type, year, last_value)
		values ($1, $2, $3, $4)
		on conflict (tenant_id, doc_type, year) do update
		set last_value = greatest(document_series.last_value, excluded.last_value), updated_at = now()`
	for k, n := range lift {
		if _, err := s.db.Exec(ctx, q, tenantID, k.docType, k.year, n); err != nil {
			return fmt.Errorf("lift the %s series for %d: %w", k.docType, k.year, err)
		}
	}
	return nil
}

// insertImportRun records the file and the two totals it was accepted on.
func (s *Store) insertImportRun(ctx context.Context, tenantID, actorID, runID uuid.UUID, kind, filename, sha string, source, stored any, imported, skipped int) error {
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("encode the file's totals: %w", err)
	}
	storedJSON, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode the stored totals: %w", err)
	}

	const q = `
		insert into import_runs (id, tenant_id, kind, filename, sha256, source_totals,
			stored_totals, rows_imported, rows_skipped, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	if _, err := s.db.Exec(ctx, q, runID, tenantID, kind, filename, sha,
		sourceJSON, storedJSON, imported, skipped, actorID); err != nil {
		return fmt.Errorf("record the import run: %w", err)
	}
	return nil
}

// stockMismatch says how a unit the app already has differs from the sheet's
// row, or "" when they agree.
func stockMismatch(v Vehicle, u legacy.Unit) string {
	switch {
	case v.PriceCents != u.PriceCents:
		return fmt.Sprintf("chassis %s is already in stock at a price of %s, but the sheet says %s",
			u.ChassisNo, money.Format(v.PriceCents), money.Format(u.PriceCents))
	case u.CostCents > 0 && v.CostCents != u.CostCents:
		return fmt.Sprintf("chassis %s is already in stock at a cost of %s, but the sheet says %s",
			u.ChassisNo, money.Format(v.CostCents), money.Format(u.CostCents))
	case v.Status != StatusAvailable && v.Status != StatusReserved:
		return fmt.Sprintf("the sheet lists chassis %s, which the app has as %s",
			u.ChassisNo, strings.ToLower(v.Status))
	case u.Status == legacy.StockAvailable && v.Status == StatusReserved:
		return fmt.Sprintf("the sheet lists chassis %s as available, but it is held for a customer",
			u.ChassisNo)
	}
	return ""
}

// ImportRun is one recorded import, as the history screen reads it.
type ImportRun struct {
	ID           uuid.UUID
	Kind         string
	Filename     string
	SHA256       string
	RowsImported int
	RowsSkipped  int
	ActorName    *string
	CreatedAt    time.Time
}

// ImportNumbers is one side of an import's arithmetic, in one shape for both
// kinds of file: how many rows agree, what they are worth, and the cost of a
// unit, which only the stock sheet carries. The down payment and the balance
// are the old app's own split of a sale, and they are kept because a file and a
// book that agree on the price but not on the deposit were never the same book.
type ImportNumbers struct {
	Count        int
	ValueCents   int64
	DownCents    int64
	BalanceCents int64
	CostCents    int64
}

// ImportRunDetail is one recorded run together with the two totals it was
// accepted on. Reading them back from the run row is what lets a page say what
// the database holds rather than what the handler remembered.
type ImportRunDetail struct {
	ImportRun
	Source ImportNumbers
	Stored ImportNumbers
}

// ImportRunByID reads one run and its totals. Must run inside InTenant.
func (s *Store) ImportRunByID(ctx context.Context, tenantID, id uuid.UUID) (ImportRunDetail, error) {
	const q = `
		select r.id, r.kind, r.filename, r.sha256, r.rows_imported, r.rows_skipped,
			r.source_totals, r.stored_totals, u.name, r.created_at
		from import_runs r
		left join users u on u.id = r.created_by
		where r.tenant_id = $1 and r.id = $2`

	var (
		d              ImportRunDetail
		source, stored []byte
	)
	err := s.db.QueryRow(ctx, q, tenantID, id).Scan(&d.ID, &d.Kind, &d.Filename, &d.SHA256,
		&d.RowsImported, &d.RowsSkipped, &source, &stored, &d.ActorName, &d.CreatedAt)
	if err != nil {
		return ImportRunDetail{}, fmt.Errorf("read the import run: %w", notFound(err))
	}
	if d.Source, err = decodeImportNumbers(source, d.Kind); err != nil {
		return ImportRunDetail{}, err
	}
	if d.Stored, err = decodeImportNumbers(stored, d.Kind); err != nil {
		return ImportRunDetail{}, err
	}
	return d, nil
}

// decodeImportNumbers reads one totals blob. The json names are the ones the
// legacy totals carry, so the stored row and a freshly parsed file are the same
// shape and can be compared without a translation table.
func decodeImportNumbers(raw []byte, kind string) (ImportNumbers, error) {
	var t struct {
		Papers       int   `json:"papers"`
		Units        int   `json:"units"`
		GrossCents   int64 `json:"gross_cents"`
		PriceCents   int64 `json:"price_cents"`
		DownCents    int64 `json:"down_cents"`
		BalanceCents int64 `json:"balance_cents"`
		CostCents    int64 `json:"cost_cents"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return ImportNumbers{}, fmt.Errorf("read the import totals: %w", err)
	}
	if kind == ImportStock {
		return ImportNumbers{Count: t.Units, ValueCents: t.PriceCents, CostCents: t.CostCents}, nil
	}
	return ImportNumbers{
		Count:        t.Papers,
		ValueCents:   t.GrossCents,
		DownCents:    t.DownCents,
		BalanceCents: t.BalanceCents,
	}, nil
}

// ListImportRuns must run inside InTenant.
func (s *Store) ListImportRuns(ctx context.Context, tenantID uuid.UUID, limit int) ([]ImportRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		select r.id, r.kind, r.filename, r.sha256, r.rows_imported, r.rows_skipped,
			u.name, r.created_at
		from import_runs r
		left join users u on u.id = r.created_by
		where r.tenant_id = $1
		order by r.created_at desc, r.id desc
		limit $2`

	rows, err := s.db.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list import runs: %w", err)
	}
	defer rows.Close()

	var out []ImportRun
	for rows.Next() {
		var r ImportRun
		if err := rows.Scan(&r.ID, &r.Kind, &r.Filename, &r.SHA256, &r.RowsImported,
			&r.RowsSkipped, &r.ActorName, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan import run: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list import runs: %w", err)
	}
	return out, nil
}

// ImportKindLabel spells an import kind out for a sentence.
func ImportKindLabel(kind string) string {
	switch kind {
	case ImportInvoices:
		return "invoices file"
	case ImportStock:
		return "stock sheet"
	}
	return kind
}
