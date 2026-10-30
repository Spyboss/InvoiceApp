package web

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// reportsIndex sends /reports to the report a person opens first.
func (s *Server) reportsIndex() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/reports/sales", http.StatusSeeOther)
	}
}

// reportScope is the filter a report reads under: the period, and whether the
// person may see everyone's documents or only their own.
type reportScope struct {
	From      time.Time
	To        time.Time
	CreatedBy *uuid.UUID
	ReadAll   bool
}

// newReportScope reads the period from the query. It defaults to the month so
// far, which is what a manager asks for most often.
func (s *Server) newReportScope(r *http.Request) reportScope {
	today := businessToday(s.now())
	from := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := today

	if v := optionalDate(r.URL.Query().Get("from")); !v.IsZero() {
		from = v
	}
	if v := optionalDate(r.URL.Query().Get("to")); !v.IsZero() {
		to = v
	}

	scope := reportScope{From: from, To: to, ReadAll: auth.Can(r.Context(), "document.read.all")}
	if !scope.ReadAll {
		if u, ok := auth.User(r.Context()); ok {
			id := u.User.ID
			scope.CreatedBy = &id
		}
	}
	return scope
}

func (s *Server) salesReportPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		scope := s.newReportScope(r)
		var report store.SalesReport
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			report, err = q.SalesReportFor(r.Context(), t.ID, scope.From, scope.To, scope.CreatedBy)
			return err
		})
		if err != nil {
			s.internalError(w, r, "sales report", err)
			return
		}

		s.render(w, r, http.StatusOK, views.SalesReport(views.SalesReportData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			From:       scope.From.Format("2006-01-02"),
			To:         scope.To.Format("2006-01-02"),
			Invalid:    scope.From.After(scope.To),
			ReadAll:    scope.ReadAll,
			Types:      salesTypeRows(report.ByType),
			Users:      salesUserRows(report.ByUser),
			Total: views.SalesTotalRow{
				Count:   report.Total.Count,
				Gross:   money.Format(report.Total.GrossCents),
				Down:    money.Format(report.Total.DownCents),
				Balance: money.Format(report.Total.BalanceCents),
			},
		}))
	}
}

func (s *Server) salesReportCSV() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		scope := s.newReportScope(r)
		var report store.SalesReport
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			report, err = q.SalesReportFor(r.Context(), t.ID, scope.From, scope.To, scope.CreatedBy)
			return err
		})
		if err != nil {
			s.internalError(w, r, "sales report csv", err)
			return
		}

		rows := make([][]string, 0, len(report.ByType)+len(report.ByUser)+1)
		for _, row := range report.ByType {
			rows = append(rows, []string{"Type", documentTypeLabel(row.DocType), csvCount(row.Count),
				money.Plain(row.GrossCents), money.Plain(row.DownCents), money.Plain(row.BalanceCents)})
		}
		for _, row := range report.ByUser {
			rows = append(rows, []string{"User", row.UserName, csvCount(row.Count),
				money.Plain(row.GrossCents), money.Plain(row.DownCents), money.Plain(row.BalanceCents)})
		}
		rows = append(rows, []string{"Total", "", csvCount(report.Total.Count),
			money.Plain(report.Total.GrossCents), money.Plain(report.Total.DownCents), money.Plain(report.Total.BalanceCents)})

		s.writeCSV(w, r, csvName("sales", scope.From.Format("2006-01-02"), scope.To.Format("2006-01-02")),
			[]string{"Group", "Name", "Invoices", "Gross", "Advance", "Balance"}, rows)
	}
}

func (s *Server) receivablesReportPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		asOf := businessToday(s.now())
		if v := optionalDate(r.URL.Query().Get("as_of")); !v.IsZero() {
			asOf = v
		}
		createdBy, readAll := s.reportOwner(r)

		var report store.ReceivablesReport
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			report, err = q.ReceivablesReportFor(r.Context(), t.ID, asOf, createdBy)
			return err
		})
		if err != nil {
			s.internalError(w, r, "receivables report", err)
			return
		}

		s.render(w, r, http.StatusOK, views.ReceivablesReport(views.ReceivablesReportData{
			TenantName:     t.Name,
			UserName:       u.User.Name,
			Nav:            navLinks(r.Context(), r.URL.Path),
			AsOf:           asOf.Format("2006-01-02"),
			ReadAll:        readAll,
			Rows:           receivableRows(report.Rows, true),
			Buckets:        receivableBucketRows(report.Buckets),
			ByFinance:      receivableFinanceRows(report.ByFinance),
			Total:          money.Format(report.TotalOutstandingCents),
			Count:          len(report.Rows),
			HasOutstanding: report.TotalOutstandingCents > 0,
		}))
	}
}

func (s *Server) receivablesReportCSV() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		asOf := businessToday(s.now())
		if v := optionalDate(r.URL.Query().Get("as_of")); !v.IsZero() {
			asOf = v
		}
		createdBy, _ := s.reportOwner(r)

		var report store.ReceivablesReport
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			report, err = q.ReceivablesReportFor(r.Context(), t.ID, asOf, createdBy)
			return err
		})
		if err != nil {
			s.internalError(w, r, "receivables report csv", err)
			return
		}

		rows := make([][]string, 0, len(report.Rows)+len(report.Buckets)+len(report.ByFinance)+1)
		for _, row := range report.Rows {
			rows = append(rows, []string{
				"Invoice", row.DocNo, row.DocDate.Format("2006-01-02"), row.CustomerName,
				row.FinanceCompany, csvCount(row.Days), money.Plain(row.BalanceCents),
				money.Plain(row.PaidCents), money.Plain(row.OutstandingCents), store.AgeBucket(row.Days),
			})
		}
		for _, row := range report.Buckets {
			rows = append(rows, []string{"Bucket", row.Bucket, "", "", "", csvCount(row.Count), "", "",
				money.Plain(row.OutstandingCents), row.Bucket})
		}
		for _, row := range report.ByFinance {
			rows = append(rows, []string{"Finance", row.FinanceCompany, "", "", row.FinanceCompany, csvCount(row.Count),
				"", "", money.Plain(row.OutstandingCents), ""})
		}
		rows = append(rows, []string{"Total", "", "", "", "", csvCount(len(report.Rows)), "", "",
			money.Plain(report.TotalOutstandingCents), ""})

		s.writeCSV(w, r, csvName("receivables", asOf.Format("2006-01-02")),
			[]string{"Group", "Name", "Date", "Customer", "Finance", "Count or days", "Balance at issue",
				"Paid since", "Outstanding", "Age bucket"}, rows)
	}
}

func (s *Server) stockReportPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		asOf := businessToday(s.now())
		// Cost is what the dealership paid, so it follows the screen that
		// records it: owner and manager. A salesperson sees the yard and the
		// price, never the margin.
		includeCost := auth.Can(r.Context(), "vehicle.write")

		var report store.StockReport
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			report, err = q.StockReportFor(r.Context(), t.ID, asOf, includeCost)
			return err
		})
		if err != nil {
			s.internalError(w, r, "stock report", err)
			return
		}

		s.render(w, r, http.StatusOK, views.StockReport(views.StockReportData{
			TenantName:  t.Name,
			UserName:    u.User.Name,
			Nav:         navLinks(r.Context(), r.URL.Path),
			AsOf:        asOf.Format("2006-01-02"),
			IncludeCost: includeCost,
			Statuses:    stockStatusRows(report.Statuses),
			Ageing:      stockAgeRows(report.Ageing),
			OnHand:      report.OnHand,
			OnHandCost:  money.Format(report.OnHandCost),
			OnHandSale:  money.Format(report.OnHandSale),
			EmptyYard:   len(report.Statuses) == 0,
		}))
	}
}

func (s *Server) stockReportCSV() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		asOf := businessToday(s.now())
		includeCost := auth.Can(r.Context(), "vehicle.write")

		var report store.StockReport
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			report, err = q.StockReportFor(r.Context(), t.ID, asOf, includeCost)
			return err
		})
		if err != nil {
			s.internalError(w, r, "stock report csv", err)
			return
		}

		rows := make([][]string, 0, len(report.Statuses)+len(report.Ageing)+1)
		for _, row := range report.Statuses {
			rows = append(rows, []string{"Status", row.Status, csvCount(row.Count),
				money.Plain(row.CostCents), money.Plain(row.PriceCents), ""})
		}
		for _, row := range report.Ageing {
			rows = append(rows, []string{"Ageing", row.Bucket, csvCount(row.Count), "", money.Plain(row.PriceCents), ""})
		}
		rows = append(rows, []string{"On hand", "", csvCount(report.OnHand),
			money.Plain(report.OnHandCost), money.Plain(report.OnHandSale), ""})

		s.writeCSV(w, r, csvName("stock", asOf.Format("2006-01-02")),
			[]string{"Group", "Name", "Units", "Cost", "Price", "Note"}, rows)
	}
}

// reportOwner is who a money report is allowed to show: everyone for a person
// who reads every document, otherwise only what that person issued.
func (s *Server) reportOwner(r *http.Request) (*uuid.UUID, bool) {
	if auth.Can(r.Context(), "document.read.all") {
		return nil, true
	}
	u, ok := auth.User(r.Context())
	if !ok {
		return nil, false
	}
	id := u.User.ID
	return &id, false
}

func salesTypeRows(rows []store.SalesTypeRow) []views.SalesReportRow {
	out := make([]views.SalesReportRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.SalesReportRow{
			Name:    documentTypeLabel(row.DocType),
			Count:   row.Count,
			Gross:   money.Format(row.GrossCents),
			Down:    money.Format(row.DownCents),
			Balance: money.Format(row.BalanceCents),
		})
	}
	return out
}

func salesUserRows(rows []store.SalesUserRow) []views.SalesReportRow {
	out := make([]views.SalesReportRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.SalesReportRow{
			Name:    row.UserName,
			Count:   row.Count,
			Gross:   money.Format(row.GrossCents),
			Down:    money.Format(row.DownCents),
			Balance: money.Format(row.BalanceCents),
		})
	}
	return out
}

// receivableRows formats the invoice list. With ages, it also names the bucket
// each invoice falls in, so the table and the summary below agree.
func receivableRows(rows []store.Receivable, withAge bool) []views.ReceivableRow {
	out := make([]views.ReceivableRow, 0, len(rows))
	for _, row := range rows {
		item := views.ReceivableRow{
			DocumentID:  row.DocumentID.String(),
			DocNo:       row.DocNo,
			Date:        row.DocDate.Format("02/01/2006"),
			Customer:    row.CustomerName,
			Finance:     row.FinanceCompany,
			Balance:     money.Format(row.BalanceCents),
			Paid:        money.Format(row.PaidCents),
			Outstanding: money.Format(row.OutstandingCents),
			Days:        row.Days,
		}
		if withAge {
			item.Bucket = store.AgeBucket(row.Days)
		}
		out = append(out, item)
	}
	return out
}

func receivableBucketRows(rows []store.ReceivableBucketRow) []views.ReceivableBucketRow {
	out := make([]views.ReceivableBucketRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ReceivableBucketRow{
			Bucket:      row.Bucket,
			Count:       row.Count,
			Outstanding: money.Format(row.OutstandingCents),
		})
	}
	return out
}

func receivableFinanceRows(rows []store.ReceivableFinanceRow) []views.ReceivableFinanceRow {
	out := make([]views.ReceivableFinanceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ReceivableFinanceRow{
			Finance:     row.FinanceCompany,
			Count:       row.Count,
			Outstanding: money.Format(row.OutstandingCents),
		})
	}
	return out
}

func stockStatusRows(rows []store.StockStatusRow) []views.StockStatusRow {
	out := make([]views.StockStatusRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.StockStatusRow{
			Status: row.Status,
			Count:  row.Count,
			Cost:   money.Format(row.CostCents),
			Price:  money.Format(row.PriceCents),
		})
	}
	return out
}

func stockAgeRows(rows []store.StockAgeRow) []views.StockAgeRow {
	out := make([]views.StockAgeRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.StockAgeRow{
			Bucket: row.Bucket,
			Count:  row.Count,
			Price:  money.Format(row.PriceCents),
		})
	}
	return out
}
