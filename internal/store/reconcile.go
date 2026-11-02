package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/legacy"
	"github.com/spyboss/invoiceapp/internal/money"
)

// ReconcileSide is one half of a comparison: the numbers a file and the book
// both produce for one day of one document type. The four are kept apart
// because a sale whose price agrees but whose down payment moved is still a
// difference, and one total would hide it.
type ReconcileSide struct {
	Papers       int
	GrossCents   int64
	DownCents    int64
	BalanceCents int64
}

// Any reports whether the side counted anything.
func (s ReconcileSide) Any() bool { return s.Papers > 0 || s.GrossCents != 0 }

// ReconcileLine is one day of one document type. The difference is stored minus
// source, and the database computes it, so a line cannot claim a match.
type ReconcileLine struct {
	Day        time.Time
	DocType    string
	Source     ReconcileSide
	Stored     ReconcileSide
	Difference ReconcileSide
}

// ReconcileRun is the summary of one comparison over a range of days.
type ReconcileRun struct {
	ID         uuid.UUID
	Filename   string
	SHA256     string
	DateFrom   time.Time
	DateTo     time.Time
	Difference ReconcileSide
	// Matched is decided by the database from the four differences above.
	Matched   bool
	ActorName *string
	CreatedAt time.Time
}

// ReconcileParams is one file checked against the book over a range of days.
type ReconcileParams struct {
	Filename string
	SHA256   string
	File     legacy.InvoiceFile
	DateFrom time.Time
	DateTo   time.Time
}

// ReconcileResult is the run plus every line it compared.
type ReconcileResult struct {
	Run   ReconcileRun
	Lines []ReconcileLine
}

// Reconcile checks a file the old app wrote against the papers this app holds
// for the same days, and records the answer. It only compares papers that came
// in from a file: a paper this app issued itself is not in the file, so it is
// not a difference. A run that finds one writes an alert in the same
// transaction. Must run inside InTenant.
func (s *Store) Reconcile(ctx context.Context, tenantID, actorID uuid.UUID, p ReconcileParams) (ReconcileResult, error) {
	var zero ReconcileResult
	if tenantID == uuid.Nil || actorID == uuid.Nil {
		return zero, errors.New("store: a reconciliation needs a tenant and an actor")
	}
	if problems := reconcileProblems(p); len(problems) > 0 {
		return zero, &ImportRefusal{Problems: problems}
	}

	from := truncateDate(p.DateFrom)
	to := truncateDate(p.DateTo)

	// What the file says, per day and type, is held in memory: it is the thing
	// being checked, not something the database wrote.
	type dayType struct {
		day     string
		docType string
	}
	source := make(map[dayType]ReconcileSide, len(p.File.Papers))
	days := make(map[dayType]time.Time, len(p.File.Papers))
	for _, paper := range p.File.Papers {
		day := truncateDate(paper.Date)
		if day.Before(from) || day.After(to) {
			continue
		}
		key := dayType{day: day.Format("2006-01-02"), docType: paperDocType(paper)}
		side := source[key]
		side.Papers++
		side.GrossCents += paper.PriceCents
		side.DownCents += paper.DownCents
		side.BalanceCents += paper.BalanceCents
		source[key] = side
		days[key] = day
	}

	// What the book holds for those days, read from the rows rather than from
	// anything this handler remembers.
	const storedQ = `
		select doc_date, doc_type, count(*), coalesce(sum(price_cents), 0),
			coalesce(sum(down_cents), 0), coalesce(sum(balance_cents), 0)
		from documents
		where tenant_id = $1 and imported_at is not null
		  and doc_date between $2 and $3
		group by doc_date, doc_type`
	rows, err := s.db.Query(ctx, storedQ, tenantID, from, to)
	if err != nil {
		return zero, fmt.Errorf("read the book for the range: %w", err)
	}
	stored := make(map[dayType]ReconcileSide)
	for rows.Next() {
		var (
			day     time.Time
			docType string
			side    ReconcileSide
		)
		if err := rows.Scan(&day, &docType, &side.Papers, &side.GrossCents, &side.DownCents,
			&side.BalanceCents); err != nil {
			rows.Close()
			return zero, fmt.Errorf("read the book for the range: %w", err)
		}
		key := dayType{day: day.Format("2006-01-02"), docType: docType}
		stored[key] = side
		days[key] = day
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return zero, fmt.Errorf("read the book for the range: %w", err)
	}
	rows.Close()

	keys := make([]dayType, 0, len(source)+len(stored))
	for key := range source {
		keys = append(keys, key)
	}
	for key := range stored {
		if _, both := source[key]; !both {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return zero, &ImportRefusal{Problems: []ImportProblem{{Problem: fmt.Sprintf(
			"neither the file nor the book has a paper between %s and %s",
			from.Format("2006-01-02"), to.Format("2006-01-02"))}}}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].day != keys[j].day {
			return keys[i].day < keys[j].day
		}
		return keys[i].docType < keys[j].docType
	})

	runID, err := uuid.NewV7()
	if err != nil {
		return zero, fmt.Errorf("reconciliation id: %w", err)
	}

	const lineQ = `
		insert into reconciliation_lines (run_id, tenant_id, day, doc_type,
			source_papers, source_gross, source_down, source_balance,
			stored_papers, stored_gross, stored_down, stored_balance)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	for _, key := range keys {
		src, held := source[key], stored[key]
		if _, err := s.db.Exec(ctx, lineQ, runID, tenantID, days[key], key.docType,
			src.Papers, src.GrossCents, src.DownCents, src.BalanceCents,
			held.Papers, held.GrossCents, held.DownCents, held.BalanceCents); err != nil {
			return zero, fmt.Errorf("record the reconciliation line: %w", err)
		}
	}

	// The run is counted from the lines it just wrote, so the summary cannot
	// disagree with the detail. matched is a generated column and is not
	// something this code can set.
	const runQ = `
		insert into reconciliation_runs (id, tenant_id, filename, sha256, date_from, date_to,
			difference_papers, difference_gross, difference_down, difference_balance, created_by)
		select $1, $2, $3, $4, $5, $6,
			coalesce(sum(difference_papers), 0), coalesce(sum(difference_gross), 0),
			coalesce(sum(difference_down), 0), coalesce(sum(difference_balance), 0), $7
		from reconciliation_lines
		where tenant_id = $2 and run_id = $1
		returning id, date_from, date_to, difference_papers, difference_gross,
			difference_down, difference_balance, matched, created_at`

	run := ReconcileRun{Filename: p.Filename, SHA256: p.SHA256}
	err = s.db.QueryRow(ctx, runQ, runID, tenantID, p.Filename, p.SHA256, from, to, actorID).
		Scan(&run.ID, &run.DateFrom, &run.DateTo, &run.Difference.Papers, &run.Difference.GrossCents,
			&run.Difference.DownCents, &run.Difference.BalanceCents, &run.Matched, &run.CreatedAt)
	if err != nil {
		return zero, fmt.Errorf("record the reconciliation: %w", err)
	}

	lines, err := s.reconciliationLines(ctx, tenantID, runID)
	if err != nil {
		return zero, err
	}
	res := ReconcileResult{Run: run, Lines: lines}
	if run.Matched {
		return res, nil
	}

	if _, err := s.RecordAlert(ctx, tenantID, AlertParams{
		Kind:     AlertReconciliation,
		Message:  reconcileMessage(p, run, lines),
		Severity: SeverityWarning,
		Entity:   from.Format("2006-01-02") + "/" + to.Format("2006-01-02"),
	}); err != nil {
		return zero, err
	}
	return res, nil
}

// reconciliationLines reads back what the database stored, so the screen shows
// the differences the database computed rather than the ones this code expects.
func (s *Store) reconciliationLines(ctx context.Context, tenantID, runID uuid.UUID) ([]ReconcileLine, error) {
	const q = `
		select day, doc_type, source_papers, source_gross, source_down, source_balance,
			stored_papers, stored_gross, stored_down, stored_balance,
			difference_papers, difference_gross, difference_down, difference_balance
		from reconciliation_lines
		where tenant_id = $1 and run_id = $2
		order by day, doc_type`

	rows, err := s.db.Query(ctx, q, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("read the reconciliation lines: %w", err)
	}
	defer rows.Close()

	var out []ReconcileLine
	for rows.Next() {
		var l ReconcileLine
		if err := rows.Scan(&l.Day, &l.DocType,
			&l.Source.Papers, &l.Source.GrossCents, &l.Source.DownCents, &l.Source.BalanceCents,
			&l.Stored.Papers, &l.Stored.GrossCents, &l.Stored.DownCents, &l.Stored.BalanceCents,
			&l.Difference.Papers, &l.Difference.GrossCents, &l.Difference.DownCents,
			&l.Difference.BalanceCents); err != nil {
			return nil, fmt.Errorf("read the reconciliation lines: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the reconciliation lines: %w", err)
	}
	return out, nil
}

// reconcileProblems refuses a comparison that cannot mean anything: a range the
// wrong way round, or a file with rows this app cannot read.
func reconcileProblems(p ReconcileParams) []ImportProblem {
	var problems []ImportProblem
	add := func(format string, args ...any) {
		problems = append(problems, ImportProblem{Problem: fmt.Sprintf(format, args...)})
	}

	switch {
	case p.DateFrom.IsZero() || p.DateTo.IsZero():
		add("say which days to check")
	case p.DateFrom.After(p.DateTo):
		add("the first day is after the last day")
	case p.DateFrom.Year() < 2000 || p.DateTo.Year() > 2100:
		add("the range must sit between the years 2000 and 2100")
	}
	if tooLong(p.Filename, 200) {
		add("the file name is longer than 200 characters")
	}
	// The hash is what makes the run provable: without it a row in a month's
	// time cannot say which file the book was compared with.
	if len(p.SHA256) != 64 {
		add("the file contents could not be identified, so the comparison cannot be proven later")
	}
	for _, problem := range p.File.Problems {
		problems = append(problems, ImportProblem{Line: problem.Line, Problem: problem.Problem})
	}
	return problems
}

// reconcileMessage says what a run found in one sentence, pointing at the first
// day that differs so somebody can go and look at it.
func reconcileMessage(p ReconcileParams, run ReconcileRun, lines []ReconcileLine) string {
	var first *ReconcileLine
	for i := range lines {
		if lines[i].Difference.Papers != 0 || lines[i].Difference.GrossCents != 0 ||
			lines[i].Difference.DownCents != 0 || lines[i].Difference.BalanceCents != 0 {
			first = &lines[i]
			break
		}
	}

	message := fmt.Sprintf("Reconciling %s for %s to %s found the book has %s",
		p.Filename, run.DateFrom.Format("2006-01-02"), run.DateTo.Format("2006-01-02"),
		run.Difference.Description())
	if first != nil {
		message += fmt.Sprintf(" The first difference is %s, %s.",
			first.Day.Format("2006-01-02"), SeriesLabel(first.DocType))
	}
	return message
}

// Description puts one difference into words. A count and an amount are said
// separately, because "3 more papers" and "3 more papers worth less money" are
// different problems, and the alert and the screen both read this so they
// cannot describe the same difference two ways.
func (d ReconcileSide) Description() string {
	count := "the same number of papers as the file"
	if n := d.Papers; n != 0 {
		word := "papers"
		if n == 1 || n == -1 {
			word = "paper"
		}
		direction := "more"
		if n < 0 {
			direction, n = "fewer", -n
		}
		count = fmt.Sprintf("%d %s %s than the file", n, direction, word)
	}

	amount := "the money agrees"
	switch {
	case d.GrossCents > 0:
		amount = "worth " + money.Format(d.GrossCents) + " more"
	case d.GrossCents < 0:
		amount = "worth " + money.Format(-d.GrossCents) + " less"
	}
	return count + ", " + amount + "."
}

// ReconciliationRunByID reads one recorded run and every line it compared, so
// a page can show the differences the database computed rather than the ones a
// handler expected. Must run inside InTenant.
func (s *Store) ReconciliationRunByID(ctx context.Context, tenantID, id uuid.UUID) (ReconcileResult, error) {
	const q = `
		select r.id, r.filename, r.sha256, r.date_from, r.date_to, r.difference_papers,
			r.difference_gross, r.difference_down, r.difference_balance, r.matched, u.name, r.created_at
		from reconciliation_runs r
		left join users u on u.id = r.created_by
		where r.tenant_id = $1 and r.id = $2`

	var run ReconcileRun
	err := s.db.QueryRow(ctx, q, tenantID, id).Scan(&run.ID, &run.Filename, &run.SHA256,
		&run.DateFrom, &run.DateTo, &run.Difference.Papers, &run.Difference.GrossCents,
		&run.Difference.DownCents, &run.Difference.BalanceCents, &run.Matched,
		&run.ActorName, &run.CreatedAt)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("read the reconciliation: %w", notFound(err))
	}

	lines, err := s.reconciliationLines(ctx, tenantID, id)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Run: run, Lines: lines}, nil
}

// ListReconciliationRuns must run inside InTenant.
func (s *Store) ListReconciliationRuns(ctx context.Context, tenantID uuid.UUID, limit int) ([]ReconcileRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		select r.id, r.filename, r.date_from, r.date_to, r.difference_papers, r.difference_gross,
			r.difference_down, r.difference_balance, r.matched, u.name, r.created_at
		from reconciliation_runs r
		left join users u on u.id = r.created_by
		where r.tenant_id = $1
		order by r.created_at desc, r.id desc
		limit $2`

	rows, err := s.db.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list reconciliation runs: %w", err)
	}
	defer rows.Close()

	var out []ReconcileRun
	for rows.Next() {
		var r ReconcileRun
		if err := rows.Scan(&r.ID, &r.Filename, &r.DateFrom, &r.DateTo, &r.Difference.Papers,
			&r.Difference.GrossCents, &r.Difference.DownCents, &r.Difference.BalanceCents,
			&r.Matched, &r.ActorName, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reconciliation run: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reconciliation runs: %w", err)
	}
	return out, nil
}
