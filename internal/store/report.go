package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Ageing buckets, oldest last. One definition, used by the stock report and by
// receivables, so two screens never disagree about what "60 days" means.
var ageBuckets = []struct {
	label string
	max   int
}{
	{"0-30", 30},
	{"31-60", 60},
	{"61-90", 90},
	{"90+", 1 << 30},
}

// AgeBucket names the bucket a number of days falls in.
func AgeBucket(days int) string {
	if days < 0 {
		days = 0
	}
	for _, b := range ageBuckets {
		if days <= b.max {
			return b.label
		}
	}
	return ageBuckets[len(ageBuckets)-1].label
}

// SalesTypeRow is one invoice type in a period.
type SalesTypeRow struct {
	DocType      string
	Count        int
	GrossCents   int64
	DownCents    int64
	BalanceCents int64
}

// SalesUserRow is what one person sold in a period.
type SalesUserRow struct {
	UserName     string
	Count        int
	GrossCents   int64
	DownCents    int64
	BalanceCents int64
}

// SalesTotal is the period in one line.
type SalesTotal struct {
	Count        int
	GrossCents   int64
	DownCents    int64
	BalanceCents int64
}

// SalesReport is what was invoiced between two business dates, by type and by
// user. Quotations and advance receipts are not sales, so they do not appear.
type SalesReport struct {
	From   time.Time
	To     time.Time
	ByType []SalesTypeRow
	ByUser []SalesUserRow
	Total  SalesTotal
}

// SalesReportFor reads what was invoiced in a period. A clerk who may not read
// every document passes their own id, so the report shows what they sold and
// not what their colleagues sold. Must run inside InTenant.
func (s *Store) SalesReportFor(ctx context.Context, tenantID uuid.UUID, from, to time.Time, createdBy *uuid.UUID) (SalesReport, error) {
	report := SalesReport{From: from, To: to}

	const byType = `
		select doc_type, count(*), coalesce(sum(price_cents), 0),
		       coalesce(sum(down_cents), 0), coalesce(sum(balance_cents), 0)
		from documents
		where tenant_id = $1 and doc_date >= $2 and doc_date <= $3
		  and doc_type in ('SALES_CASH', 'SALES_LEASING')
		  and ($4::uuid is null or created_by = $4)
		group by doc_type
		order by case doc_type when 'SALES_CASH' then 1 else 2 end`

	rows, err := s.db.Query(ctx, byType, tenantID, from, to, createdBy)
	if err != nil {
		return SalesReport{}, fmt.Errorf("sales by type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row SalesTypeRow
		if err := rows.Scan(&row.DocType, &row.Count, &row.GrossCents, &row.DownCents, &row.BalanceCents); err != nil {
			return SalesReport{}, fmt.Errorf("scan sales by type: %w", err)
		}
		report.ByType = append(report.ByType, row)
		report.Total.Count += row.Count
		report.Total.GrossCents += row.GrossCents
		report.Total.DownCents += row.DownCents
		report.Total.BalanceCents += row.BalanceCents
	}
	if err := rows.Err(); err != nil {
		return SalesReport{}, fmt.Errorf("sales by type: %w", err)
	}

	const byUser = `
		select coalesce(u.name, 'removed user'), count(*), coalesce(sum(d.price_cents), 0),
		       coalesce(sum(d.down_cents), 0), coalesce(sum(d.balance_cents), 0)
		from documents d
		left join users u on u.id = d.created_by
		where d.tenant_id = $1 and d.doc_date >= $2 and d.doc_date <= $3
		  and d.doc_type in ('SALES_CASH', 'SALES_LEASING')
		  and ($4::uuid is null or d.created_by = $4)
		group by u.id, u.name
		order by 3 desc, 1`

	userRows, err := s.db.Query(ctx, byUser, tenantID, from, to, createdBy)
	if err != nil {
		return SalesReport{}, fmt.Errorf("sales by user: %w", err)
	}
	defer userRows.Close()
	for userRows.Next() {
		var row SalesUserRow
		if err := userRows.Scan(&row.UserName, &row.Count, &row.GrossCents, &row.DownCents, &row.BalanceCents); err != nil {
			return SalesReport{}, fmt.Errorf("scan sales by user: %w", err)
		}
		report.ByUser = append(report.ByUser, row)
	}
	if err := userRows.Err(); err != nil {
		return SalesReport{}, fmt.Errorf("sales by user: %w", err)
	}

	return report, nil
}

// StockStatusRow is one status in the yard, valued at what it cost and at what
// it is priced at.
type StockStatusRow struct {
	Status     string
	Count      int
	CostCents  int64
	PriceCents int64
}

// StockAgeRow is how long the units still in the yard have been standing there.
type StockAgeRow struct {
	Bucket     string
	Count      int
	PriceCents int64
}

// StockReport is the yard as it stands: every status with its count and value,
// and the on-hand units aged. A sold unit has left, so it is counted but not
// aged.
type StockReport struct {
	AsOf       time.Time
	Statuses   []StockStatusRow
	Ageing     []StockAgeRow
	OnHand     int
	OnHandCost int64
	OnHandSale int64
}

// StockReportFor must run inside InTenant.
func (s *Store) StockReportFor(ctx context.Context, tenantID uuid.UUID, asOf time.Time, includeCost bool) (StockReport, error) {
	const q = `select status, received_at, cost_cents, price_cents
		from vehicles where tenant_id = $1 and deleted_at is null`

	rows, err := s.db.Query(ctx, q, tenantID)
	if err != nil {
		return StockReport{}, fmt.Errorf("stock report: %w", err)
	}
	defer rows.Close()

	report := StockReport{AsOf: asOf}
	statuses := make(map[string]*StockStatusRow)
	ageing := make(map[string]*StockAgeRow)

	// Fixed order so the report reads the same way every time.
	statusOrder := []string{StatusAvailable, StatusReserved, StatusReturned, StatusSold, StatusDelivered}

	for rows.Next() {
		var (
			status     string
			receivedAt time.Time
			cost, sale int64
		)
		if err := rows.Scan(&status, &receivedAt, &cost, &sale); err != nil {
			return StockReport{}, fmt.Errorf("scan stock report: %w", err)
		}

		row, seen := statuses[status]
		if !seen {
			row = &StockStatusRow{Status: status}
			statuses[status] = row
		}
		row.Count++
		row.PriceCents += sale
		if includeCost {
			row.CostCents += cost
		}

		if status == StatusSold || status == StatusDelivered {
			continue
		}
		report.OnHand++
		report.OnHandSale += sale
		if includeCost {
			report.OnHandCost += cost
		}

		days := int(asOf.Sub(receivedAt).Hours() / 24)
		bucket := AgeBucket(days)
		age, seen := ageing[bucket]
		if !seen {
			age = &StockAgeRow{Bucket: bucket}
			ageing[bucket] = age
		}
		age.Count++
		age.PriceCents += sale
	}
	if err := rows.Err(); err != nil {
		return StockReport{}, fmt.Errorf("stock report: %w", err)
	}

	for _, status := range statusOrder {
		if row, ok := statuses[status]; ok {
			report.Statuses = append(report.Statuses, *row)
			delete(statuses, status)
		}
	}
	// A status the database allows but this list forgot still gets a row.
	for _, row := range statuses {
		report.Statuses = append(report.Statuses, *row)
	}

	for _, b := range ageBuckets {
		if row, ok := ageing[b.label]; ok {
			report.Ageing = append(report.Ageing, *row)
		}
	}

	return report, nil
}

// ReceivableBucketRow is one ageing bucket of unpaid invoices.
type ReceivableBucketRow struct {
	Bucket           string
	Count            int
	OutstandingCents int64
}

// ReceivableFinanceRow is what is owed per leasing company.
type ReceivableFinanceRow struct {
	FinanceCompany   string
	Count            int
	OutstandingCents int64
}

// ReceivablesReport is the money other people still owe, aged and grouped by
// who is paying it.
type ReceivablesReport struct {
	AsOf                  time.Time
	Rows                  []Receivable
	Buckets               []ReceivableBucketRow
	ByFinance             []ReceivableFinanceRow
	TotalOutstandingCents int64
}

// ReceivablesReportFor must run inside InTenant.
func (s *Store) ReceivablesReportFor(ctx context.Context, tenantID uuid.UUID, asOf time.Time, createdBy *uuid.UUID) (ReceivablesReport, error) {
	rows, err := s.ReceivablesFor(ctx, tenantID, asOf, createdBy, 5000)
	if err != nil {
		return ReceivablesReport{}, err
	}

	report := ReceivablesReport{AsOf: asOf, Rows: rows}
	buckets := make(map[string]*ReceivableBucketRow)
	finance := make(map[string]*ReceivableFinanceRow)

	for _, r := range rows {
		report.TotalOutstandingCents += r.OutstandingCents

		label := AgeBucket(r.Days)
		bucket, ok := buckets[label]
		if !ok {
			bucket = &ReceivableBucketRow{Bucket: label}
			buckets[label] = bucket
		}
		bucket.Count++
		bucket.OutstandingCents += r.OutstandingCents

		company := r.FinanceCompany
		if company == "" {
			company = "customer"
		}
		line, ok := finance[company]
		if !ok {
			line = &ReceivableFinanceRow{FinanceCompany: company}
			finance[company] = line
		}
		line.Count++
		line.OutstandingCents += r.OutstandingCents
	}

	for _, b := range ageBuckets {
		if row, ok := buckets[b.label]; ok {
			report.Buckets = append(report.Buckets, *row)
		}
	}
	for _, row := range finance {
		report.ByFinance = append(report.ByFinance, *row)
	}
	// Largest first, ties by name, so the report does not reshuffle between
	// two loads.
	sort.Slice(report.ByFinance, func(i, j int) bool {
		a, b := report.ByFinance[i], report.ByFinance[j]
		if a.OutstandingCents != b.OutstandingCents {
			return a.OutstandingCents > b.OutstandingCents
		}
		return a.FinanceCompany < b.FinanceCompany
	})

	return report, nil
}
