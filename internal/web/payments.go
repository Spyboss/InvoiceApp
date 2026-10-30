package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) paymentsPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		method := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("method")))
		if !store.ValidPaymentMethod(method) {
			method = ""
		}
		filter := store.PaymentFilter{
			Method: method,
			Search: strings.TrimSpace(r.URL.Query().Get("q")),
			From:   optionalDate(r.URL.Query().Get("from")),
			To:     optionalDate(r.URL.Query().Get("to")),
			Limit:  200,
		}

		var payments []store.Payment
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			payments, err = q.ListPayments(r.Context(), t.ID, filter)
			return err
		})
		if err != nil {
			s.internalError(w, r, "payments", err)
			return
		}

		s.render(w, r, http.StatusOK, views.Payments(views.PaymentsData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			Query:      filter.Search,
			Method:     method,
			From:       dateValue(filter.From),
			To:         dateValue(filter.To),
			Methods:    paymentMethodOptions(method, true),
			Rows:       receiptRows(payments),
		}))
	}
}

// paymentNewPage is the receipt form for one invoice. There is deliberately no
// free standing "take money" screen: every receipt pays a named invoice, so the
// outstanding balance is checked in front of the person taking the money.
func (s *Server) paymentNewPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Invoice not found", "There is nothing at this address.")
			return
		}

		data, err := s.paymentForm(r, t, u, id, views.PaymentFormFields{}, "")
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Invoice not found", "That invoice may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "payment form", err)
			return
		}
		s.render(w, r, http.StatusOK, views.PaymentForm(data))
	}
}

func (s *Server) paymentCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields, params, documentID, message := readPaymentForm(r, businessToday(s.now()))
		// A malformed form is the clerk's typo; a refused receipt is a state
		// conflict. The status says which, so a browser or a log can tell them
		// apart.
		status := http.StatusBadRequest
		if message == "" {
			var pay store.Payment
			err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
				var err error
				pay, err = q.CreatePayment(r.Context(), t.ID, u.User.ID, params)
				if err != nil {
					return err
				}
				return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
					Action:   "payment.create",
					Entity:   "payment",
					EntityID: &pay.ID,
					After: map[string]string{
						"receipt": pay.ReceiptNo,
						"invoice": pay.DocNo,
						"method":  pay.Method,
						"amount":  money.Format(pay.AmountCents),
					},
				})
			})
			switch {
			case err == nil:
				http.Redirect(w, r, "/payments/"+pay.ID.String()+"?created=1", http.StatusSeeOther)
				return
			case errors.Is(err, store.ErrPaymentOverBalance):
				message, status = "That is more than the invoice still owes.", http.StatusConflict
			case errors.Is(err, store.ErrDayAlreadyClosed):
				// The form replaces itself with the reason, so there is
				// nothing to add here; the conflict status still says what
				// happened.
				status = http.StatusConflict
			case errors.Is(err, store.ErrInvalidPayment):
				message = "Check the amount and the payment method."
			case errors.Is(err, store.ErrNotFound):
				s.renderError(w, r, http.StatusNotFound, "Invoice not found", "That invoice may have been removed.")
				return
			default:
				s.internalError(w, r, "payment create", err)
				return
			}
		}

		// The form is re-shown with what the clerk typed, and with the balance
		// re-read so the message names the real ceiling.
		data, err := s.paymentForm(r, t, u, documentID, fields, message)
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Invoice not found", "That invoice may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "payment form", err)
			return
		}
		s.render(w, r, status, views.PaymentForm(data))
	}
}

func (s *Server) paymentPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Receipt not found", "There is nothing at this address.")
			return
		}

		pay, err := s.receipt(r, t.ID, id)
		if err != nil {
			s.renderReceiptLookupError(w, r, err)
			return
		}

		data := s.receiptDetail(r, t, u, pay)
		data.Created = r.URL.Query().Get("created") == "1"
		data.Reversed = r.URL.Query().Get("reversed") == "1"
		s.render(w, r, http.StatusOK, views.ReceiptDetail(data))
	}
}

func (s *Server) paymentReverse() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Receipt not found", "There is nothing at this address.")
			return
		}

		reason := strings.TrimSpace(r.PostFormValue("reason"))
		// The reversal is dated today, by the server. A date from the browser
		// would let somebody move cash out of a day that is already closed.
		today := businessToday(s.now())

		var pay store.Payment
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			pay, err = q.ReversePayment(r.Context(), t.ID, id, u.User.ID, reason, today)
			if err != nil {
				return err
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "payment.reverse",
				Entity:   "payment",
				EntityID: &pay.ID,
				Before:   map[string]string{"status": store.PaymentPosted},
				After: map[string]string{
					"status":  pay.Status,
					"receipt": pay.ReceiptNo,
					"amount":  money.Format(pay.AmountCents),
					"reason":  pay.ReversalReason,
				},
			})
		})
		switch {
		case err == nil:
			http.Redirect(w, r, "/payments/"+pay.ID.String()+"?reversed=1", http.StatusSeeOther)
			return
		case errors.Is(err, store.ErrDayAlreadyClosed):
			s.renderReceiptError(w, r, t, u, id, http.StatusConflict, closedDayMessage)
			return
		case errors.Is(err, store.ErrPaymentNotReversible):
			s.renderReceiptError(w, r, t, u, id, http.StatusConflict,
				"That receipt is already reversed, so it cannot be reversed again.")
			return
		case errors.Is(err, store.ErrInvalidPayment):
			s.renderReceiptError(w, r, t, u, id, http.StatusBadRequest,
				"Write why the money is being handed back. Three characters at least.")
			return
		case errors.Is(err, store.ErrNotFound):
			s.renderError(w, r, http.StatusNotFound, "Receipt not found", "That receipt may not exist.")
			return
		default:
			s.internalError(w, r, "payment reverse", err)
			return
		}
	}
}

// receipt loads one receipt. Its only caller is this file, so a missing row is
// reported the same way from every screen.
func (s *Server) receipt(r *http.Request, tenantID, id uuid.UUID) (store.Payment, error) {
	var pay store.Payment
	err := s.store.InTenant(r.Context(), tenantID, func(q *store.Store) error {
		var err error
		pay, err = q.PaymentByID(r.Context(), tenantID, id)
		return err
	})
	return pay, err
}

func (s *Server) renderReceiptLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "Receipt not found", "That receipt may not exist.")
		return
	}
	s.internalError(w, r, "receipt", err)
}

// receiptDetail formats a receipt for the screen. The read only page and the
// page that explains a refusal are the same page, so they are filled from one
// place.
func (s *Server) receiptDetail(r *http.Request, t store.Tenant, u auth.SignedIn, pay store.Payment) views.ReceiptDetailData {
	return views.ReceiptDetailData{
		TenantName:     t.Name,
		UserName:       u.User.Name,
		Nav:            navLinks(r.Context(), r.URL.Path),
		CSRFToken:      csrfToken(r.Context()),
		ID:             pay.ID.String(),
		No:             pay.ReceiptNo,
		Year:           pay.ReceiptYear,
		Date:           pay.PaidOn.Format("02/01/2006"),
		InvoiceNo:      pay.DocNo,
		InvoiceID:      pay.DocumentID.String(),
		InvoiceType:    documentTypeLabel(pay.DocType),
		CustomerName:   pay.CustomerName,
		Method:         paymentMethodLabel(pay.Method),
		Amount:         money.Format(pay.AmountCents),
		Reference:      pay.Reference,
		Notes:          pay.Notes,
		Status:         pay.Status,
		CreatedByName:  derefString(pay.CreatedByName),
		ReversedOn:     formatOptionalDate(pay.ReversedOn),
		ReversedByName: derefString(pay.ReversedByName),
		ReversalReason: pay.ReversalReason,
		CanReverse:     pay.Status == store.PaymentPosted && auth.Can(r.Context(), "payment.reverse"),
		Today:          businessToday(s.now()).Format("02/01/2006"),
	}
}

// renderReceiptError shows the receipt again with the reason it was refused.
func (s *Server) renderReceiptError(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, id uuid.UUID, status int, message string) {
	pay, err := s.receipt(r, t.ID, id)
	if err != nil {
		s.renderReceiptLookupError(w, r, err)
		return
	}
	data := s.receiptDetail(r, t, u, pay)
	data.Error = message
	s.render(w, r, status, views.ReceiptDetail(data))
}

// paymentForm builds the receipt form for one invoice. The first visit and the
// refusal after a post are the same screen, so they are filled from one place
// and can never describe the same invoice differently. An empty amount starts
// at what is still owed, which is what the clerk is about to take.
func (s *Server) paymentForm(r *http.Request, t store.Tenant, u auth.SignedIn, id uuid.UUID, fields views.PaymentFormFields, message string) (views.PaymentFormData, error) {
	doc, balance, err := s.invoiceForPayment(r.Context(), t.ID, id)
	if err != nil {
		return views.PaymentFormData{}, err
	}
	closed, err := s.dayClosed(r.Context(), t.ID)
	if err != nil {
		return views.PaymentFormData{}, err
	}

	if fields.Amount == "" {
		fields.Amount = money.Format(balance.OutstandingCents)
	}
	if fields.Method == "" {
		fields.Method = store.MethodCash
	}

	data := views.PaymentFormData{
		TenantName:    t.Name,
		UserName:      u.User.Name,
		Nav:           navLinks(r.Context(), r.URL.Path),
		CSRFToken:     csrfToken(r.Context()),
		DocumentID:    doc.ID.String(),
		DocumentNo:    doc.DocNo,
		DocumentType:  documentTypeLabel(doc.DocType),
		CustomerName:  doc.CustomerName,
		CustomerID:    doc.CustomerID.String(),
		Date:          businessToday(s.now()).Format("02/01/2006"),
		IssuedBalance: money.Format(balance.BalanceCents),
		AlreadyPaid:   money.Format(balance.PaidCents),
		Outstanding:   money.Format(balance.OutstandingCents),
		PaidInFull:    balance.OutstandingCents <= 0,
		Amount:        fields.Amount,
		Method:        fields.Method,
		Reference:     fields.Reference,
		Notes:         fields.Notes,
		Methods:       paymentMethodOptions(fields.Method, false),
		Error:         message,
	}
	if closed {
		data.ClosedNote = closedDayMessage
	}
	return data, nil
}

// closedDayMessage explains the one date rule the cashier cannot work around.
const closedDayMessage = "Today's day end is already closed, so no money can be dated today. " +
	"Record it after the next business day starts, or leave the day end until the last sale is in."

// dayClosed reports whether today's business date has already been closed.
func (s *Server) dayClosed(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	today := businessToday(s.now())
	var closed bool
	err := s.store.InTenant(ctx, tenantID, func(q *store.Store) error {
		_, err := q.DayEndOnDate(ctx, tenantID, today)
		switch {
		case err == nil:
			closed = true
			return nil
		case errors.Is(err, store.ErrNotFound):
			return nil
		}
		return err
	})
	return closed, err
}

// invoiceForPayment loads an invoice and what it still owes. Anything that is
// not a sales invoice is refused here rather than at the trigger, so the
// screen can say why in a sentence.
func (s *Server) invoiceForPayment(ctx context.Context, tenantID, id uuid.UUID) (store.Document, store.DocumentBalance, error) {
	var doc store.Document
	var balance store.DocumentBalance
	err := s.store.InTenant(ctx, tenantID, func(q *store.Store) error {
		var err error
		if doc, err = q.DocumentByID(ctx, tenantID, id); err != nil {
			return err
		}
		balance, err = q.DocumentBalanceFor(ctx, tenantID, id)
		return err
	})
	if err != nil {
		return store.Document{}, store.DocumentBalance{}, err
	}
	if doc.DocType != store.DocSalesCash && doc.DocType != store.DocSalesLease {
		return store.Document{}, store.DocumentBalance{}, store.ErrNotFound
	}
	return doc, balance, nil
}

func readPaymentForm(r *http.Request, today time.Time) (views.PaymentFormFields, store.PaymentParams, uuid.UUID, string) {
	f := views.PaymentFormFields{
		DocumentID: strings.TrimSpace(r.PostFormValue("document_id")),
		Amount:     strings.TrimSpace(r.PostFormValue("amount")),
		Method:     strings.ToUpper(strings.TrimSpace(r.PostFormValue("method"))),
		Reference:  strings.TrimSpace(r.PostFormValue("reference")),
		Notes:      strings.TrimSpace(r.PostFormValue("notes")),
	}
	documentID, parseErr := uuid.Parse(f.DocumentID)
	if parseErr != nil {
		return f, store.PaymentParams{}, uuid.Nil, "Choose the invoice this money is paying."
	}
	fail := func(message string) (views.PaymentFormFields, store.PaymentParams, uuid.UUID, string) {
		return f, store.PaymentParams{}, documentID, message
	}

	amount, err := money.Parse(f.Amount)
	if err != nil {
		return fail("Amount: " + err.Error() + ".")
	}
	if amount == 0 {
		return fail("Enter the amount that was handed over.")
	}
	if !store.ValidPaymentMethod(f.Method) {
		return fail("Choose how the money arrived.")
	}
	if utf8.RuneCountInString(f.Reference) > 60 {
		return fail("The reference is too long. 60 characters at most.")
	}
	if utf8.RuneCountInString(f.Notes) > 2000 {
		return fail("The notes are too long. 2000 characters at most.")
	}

	return f, store.PaymentParams{
		DocumentID:  documentID,
		PaidOn:      today,
		Method:      f.Method,
		AmountCents: amount,
		Reference:   f.Reference,
		Notes:       f.Notes,
	}, documentID, ""
}

func receiptRows(payments []store.Payment) []views.ReceiptRow {
	rows := make([]views.ReceiptRow, 0, len(payments))
	for _, p := range payments {
		rows = append(rows, views.ReceiptRow{
			ID:         p.ID.String(),
			No:         p.ReceiptNo,
			Date:       p.PaidOn.Format("02/01/2006"),
			InvoiceID:  p.DocumentID.String(),
			InvoiceNo:  p.DocNo,
			Customer:   p.CustomerName,
			Method:     paymentMethodLabel(p.Method),
			Amount:     money.Format(p.AmountCents),
			Status:     p.Status,
			ReversedOn: formatOptionalDate(p.ReversedOn),
		})
	}
	return rows
}

// paymentMethodLabel is the words a cashier uses, not the code in the database.
func paymentMethodLabel(method string) string {
	switch method {
	case store.MethodCash:
		return "Cash"
	case store.MethodCard:
		return "Card"
	case store.MethodBank:
		return "Bank transfer"
	case store.MethodCheque:
		return "Cheque"
	}
	return method
}

func paymentMethodOptions(selected string, includeAll bool) []views.MethodOption {
	out := make([]views.MethodOption, 0, len(store.PaymentMethods)+1)
	if includeAll {
		out = append(out, views.MethodOption{Value: "", Label: "All methods", Selected: selected == ""})
	}
	for _, m := range store.PaymentMethods {
		out = append(out, views.MethodOption{
			Value:    m,
			Label:    paymentMethodLabel(m),
			Selected: m == selected,
		})
	}
	return out
}

// optionalDate reads a yyyy-mm-dd query value, treating anything else as "no
// date" so a stale link cannot break a list.
func optionalDate(raw string) time.Time {
	at, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}
	}
	return at
}

func dateValue(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("2006-01-02")
}

func formatOptionalDate(at *time.Time) string {
	if at == nil {
		return ""
	}
	return at.Format("02/01/2006")
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
