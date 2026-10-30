package web

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// dayWindow turns a business date into the window the day end reads. Payments
// carry a business date, but the stock ledger only has instants, and the yard
// works on Colombo time even when the server does not.
func dayWindow(date time.Time) store.DayWindow {
	y, m, d := date.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, colombo)
	return store.DayWindow{Date: date, Start: start, End: start.AddDate(0, 0, 1)}
}

func (s *Server) dayEndPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		data, err := s.dayEndData(r, t, u, views.DayEndFields{}, "")
		if err != nil {
			s.internalError(w, r, "day end", err)
			return
		}
		s.render(w, r, http.StatusOK, views.DayEnd(data))
	}
}

func (s *Server) dayEndClose() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		window := dayWindow(businessToday(s.now()))
		float, counted, notes, message := readDayEndForm(r)
		status := http.StatusBadRequest
		if message == "" {
			var end store.DayEnd
			err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
				var err error
				end, err = q.CloseDay(r.Context(), t.ID, u.User.ID, store.DayEndParams{
					Window:            window,
					OpeningFloatCents: float,
					CountedCents:      counted,
					Notes:             notes,
				})
				if err != nil {
					return err
				}
				return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
					Action:   "dayend.close",
					Entity:   "day_end",
					EntityID: &end.ID,
					After: map[string]string{
						"z":        end.ZNo,
						"date":     end.BusinessDate.Format("2006-01-02"),
						"expected": money.Format(end.ExpectedCents),
						"counted":  money.Format(end.CountedCents),
						"variance": money.Format(end.VarianceCents),
					},
				})
			})
			switch {
			case err == nil:
				http.Redirect(w, r, "/day-end/"+end.ID.String()+"?closed=1", http.StatusSeeOther)
				return
			case errors.Is(err, store.ErrDayAlreadyClosed):
				// Somebody closed it while this page was open. Show them the
				// closed day rather than a bare error.
				var existing store.DayEnd
				err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
					var existingErr error
					existing, existingErr = q.DayEndOnDate(r.Context(), t.ID, window.Date)
					return existingErr
				})
				if err == nil {
					http.Redirect(w, r, "/day-end/"+existing.ID.String(), http.StatusSeeOther)
					return
				}
				message, status = "This day is already closed.", http.StatusConflict
			case errors.Is(err, store.ErrInvalidDayEnd):
				message = "Check the float and the cash you counted."
			default:
				s.internalError(w, r, "close day", err)
				return
			}
		}

		// Re-show the day with what was typed and the reason it was refused.
		data, err := s.dayEndData(r, t, u, views.DayEndFields{
			Float:   strings.TrimSpace(r.PostFormValue("opening_float")),
			Counted: strings.TrimSpace(r.PostFormValue("counted")),
			Notes:   notes,
		}, message)
		if err != nil {
			s.internalError(w, r, "day end", err)
			return
		}
		s.render(w, r, status, views.DayEnd(data))
	}
}

// dayEndData reads today's business date and builds the page. A day that is
// already closed shows its z; an open day shows what it holds so far and the
// close form. The close handler re-uses this so a refusal and a first visit
// cannot describe the day differently.
func (s *Server) dayEndData(r *http.Request, t store.Tenant, u auth.SignedIn, fields views.DayEndFields, message string) (views.DayEndData, error) {
	window := dayWindow(businessToday(s.now()))

	var closed store.DayEnd
	var isClosed bool
	var totals store.DayTotals
	var lastCounted int64
	var recent []store.DayEnd
	err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
		end, err := q.DayEndOnDate(r.Context(), t.ID, window.Date)
		switch {
		case err == nil:
			closed, isClosed = end, true
		case errors.Is(err, store.ErrNotFound):
			// The day is still open; read what it holds so far.
			if totals, err = q.DayTotalsFor(r.Context(), t.ID, window); err != nil {
				return err
			}
		default:
			return err
		}

		if lastCounted, err = q.LastCountedCash(r.Context(), t.ID); err != nil {
			return err
		}
		recent, err = q.ListDayEnds(r.Context(), t.ID, 10)
		return err
	})
	if err != nil {
		return views.DayEndData{}, err
	}

	data := views.DayEndData{
		TenantName:  t.Name,
		UserName:    u.User.Name,
		Nav:         navLinks(r.Context(), r.URL.Path),
		CSRFToken:   csrfToken(r.Context()),
		Date:        window.Date.Format("02/01/2006"),
		Open:        !isClosed,
		LastCounted: money.Format(lastCounted),
		CanClose:    auth.Can(r.Context(), "dayend.close"),
		Recent:      dayEndRows(recent),
		Error:       message,
	}
	if isClosed {
		data.Closed = dayEndView(closed)
		return data, nil
	}

	data.Totals = dayTotalsView(totals)
	// The drawer yesterday ended with this, so it is the best guess at what
	// today opened with. The cashier can change it.
	if fields.Float == "" {
		fields.Float = money.Format(lastCounted)
	}
	data.Fields = fields
	return data, nil
}

func (s *Server) dayEndDetailPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Day end not found", "There is nothing at this address.")
			return
		}

		var end store.DayEnd
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			end, err = q.DayEndByID(r.Context(), t.ID, id)
			return err
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Day end not found", "That day has not been closed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "day end", err)
			return
		}

		s.render(w, r, http.StatusOK, views.DayEndDetail(views.DayEndDetailData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			End:        *dayEndView(end),
			Closed:     r.URL.Query().Get("closed") == "1",
			Exists:     r.URL.Query().Get("exists") == "1",
		}))
	}
}

// dayEndCSV exports a closed day. The close facts and the frozen lines share
// one table so a spreadsheet can read the whole paper without a second pass,
// and the file is the same on every download because the day cannot change.
func (s *Server) dayEndCSV() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Day end not found", "There is nothing at this address.")
			return
		}

		var end store.DayEnd
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			end, err = q.DayEndByID(r.Context(), t.ID, id)
			return err
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Day end not found", "That day has not been closed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "day end csv", err)
			return
		}

		closed := dayEndView(end)
		rows := [][]string{
			{"Close", "Business date", "", end.BusinessDate.Format("02/01/2006")},
			{"Close", "Z number", "", end.ZNo},
			{"Close", "Opening float", "", money.Plain(end.OpeningFloatCents)},
			{"Close", "Cash in", "", money.Plain(end.CashInCents)},
			{"Close", "Cash handed back", "", money.Plain(end.CashOutCents)},
			{"Close", "Expected in the drawer", "", money.Plain(end.ExpectedCents)},
			{"Close", "Counted", "", money.Plain(end.CountedCents)},
			{"Close", "Variance", "", money.Plain(end.VarianceCents)},
			{"Close", "Invoices issued", csvCount(end.SalesCount), money.Plain(end.SalesTotalCents)},
			{"Close", "Receipts taken", "", money.Plain(end.PaymentsTotalCents)},
			{"Close", "Closed by", "", closed.ClosedBy},
			{"Close", "Closed at", "", closed.ClosedAt},
		}
		if end.Notes != "" {
			rows = append(rows, []string{"Close", "Notes", "", end.Notes})
		}
		for _, line := range end.Lines {
			rows = append(rows, []string{daySectionLabel(line.Section), dayLineLabel(line),
				csvCount(line.Qty), money.Plain(line.AmountCents)})
		}

		s.writeCSV(w, r, csvName("day-end", end.BusinessDate.Format("2006-01-02"), end.ZNo),
			[]string{"Section", "Item", "Count", "Amount"}, rows)
	}
}

// readDayEndForm takes the two amounts the cashier typed. Neither is trusted:
// the float is money and the counted cash is money, so both are parsed here and
// the arithmetic happens in the store.
func readDayEndForm(r *http.Request) (float, counted int64, notes, message string) {
	floatRaw := strings.TrimSpace(r.PostFormValue("opening_float"))
	countedRaw := strings.TrimSpace(r.PostFormValue("counted"))
	notes = strings.TrimSpace(r.PostFormValue("notes"))

	var err error
	if float, err = money.Parse(floatRaw); err != nil {
		if floatRaw == "" {
			return 0, 0, notes, "Enter the float the drawer opened with. Enter 0 if it was empty."
		}
		return 0, 0, notes, "Opening float: " + err.Error() + "."
	}
	if counted, err = money.Parse(countedRaw); err != nil {
		if countedRaw == "" {
			return 0, 0, notes, "Enter the cash you counted."
		}
		return 0, 0, notes, "Counted cash: " + err.Error() + "."
	}
	if utf8.RuneCountInString(notes) > 2000 {
		return 0, 0, notes, "The notes are too long. 2000 characters at most."
	}
	return float, counted, notes, ""
}

func dayEndView(end store.DayEnd) *views.DayEndView {
	// The stored variance keeps its sign, because the ledger reads it as
	// counted minus expected. The sentence on the paper says how far off the
	// drawer was, which is the size of it, not the sign.
	varianceSize := end.VarianceCents
	if varianceSize < 0 {
		varianceSize = -varianceSize
	}
	return &views.DayEndView{
		ID:            end.ID.String(),
		ZNo:           end.ZNo,
		Date:          end.BusinessDate.Format("02/01/2006"),
		OpeningFloat:  money.Format(end.OpeningFloatCents),
		CashIn:        money.Format(end.CashInCents),
		CashOut:       money.Format(end.CashOutCents),
		Expected:      money.Format(end.ExpectedCents),
		Counted:       money.Format(end.CountedCents),
		Variance:      money.Format(end.VarianceCents),
		VarianceSize:  money.Format(varianceSize),
		Short:         end.Short(),
		Exact:         end.VarianceCents == 0,
		SalesCount:    end.SalesCount,
		SalesTotal:    money.Format(end.SalesTotalCents),
		PaymentsTotal: money.Format(end.PaymentsTotalCents),
		ClosedBy:      derefString(end.ClosedByName),
		ClosedAt:      end.ClosedAt.In(colombo).Format("02/01/2006 15:04"),
		Notes:         end.Notes,
		Lines:         dayLineRows(end.Lines),
	}
}

func dayTotalsView(totals store.DayTotals) views.DayTotalsView {
	return views.DayTotalsView{
		CashIn:        money.Format(totals.CashInCents),
		CashOut:       money.Format(totals.CashOutCents),
		SalesCount:    totals.SalesCount,
		SalesTotal:    money.Format(totals.SalesTotalCents),
		PaymentsTotal: money.Format(totals.PaymentsTotalCents),
		Lines:         dayLineRows(totals.Lines()),
	}
}

func dayEndRows(ends []store.DayEnd) []views.DayEndRow {
	rows := make([]views.DayEndRow, 0, len(ends))
	for _, end := range ends {
		rows = append(rows, views.DayEndRow{
			ID:       end.ID.String(),
			ZNo:      end.ZNo,
			Date:     end.BusinessDate.Format("02/01/2006"),
			Expected: money.Format(end.ExpectedCents),
			Counted:  money.Format(end.CountedCents),
			Variance: money.Format(end.VarianceCents),
			Short:    end.Short(),
		})
	}
	return rows
}

func dayLineRows(lines []store.DayEndLine) []views.DayLineRow {
	rows := make([]views.DayLineRow, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, views.DayLineRow{
			Section:  daySectionLabel(line.Section),
			Label:    dayLineLabel(line),
			Qty:      line.Qty,
			Amount:   money.Format(line.AmountCents),
			HasMoney: line.AmountCents != 0,
		})
	}
	return rows
}

func daySectionLabel(section string) string {
	switch section {
	case store.DayPayments:
		return "Payments taken"
	case store.DayReversals:
		return "Money handed back"
	case store.DayDocuments:
		return "Documents issued"
	case store.DayStock:
		return "Stock movements"
	}
	return section
}

// dayLineLabel turns the stored code into words. The code is what the closed
// day keeps, so renaming a word later never rewrites what the paper said.
func dayLineLabel(line store.DayEndLine) string {
	switch line.Section {
	case store.DayPayments, store.DayReversals:
		return paymentMethodLabel(line.Label)
	case store.DayDocuments:
		return documentTypeLabel(line.Label)
	case store.DayStock:
		switch line.Label {
		case "RECEIVE":
			return "Received"
		case "SELL":
			return "Sold"
		case "RETURN":
			return "Returned"
		case "ADJUST":
			return "Adjusted"
		}
	}
	return line.Label
}
