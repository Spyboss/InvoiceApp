package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/pdf"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// PDFRenderer is the printing side of the app. It is an interface so the web
// tests can print without a running Gotenberg.
type PDFRenderer interface {
	Render(ctx context.Context, tenantName string, doc store.Document) ([]byte, error)
}

// colombo is the business timezone. Dates on paper are Sri Lankan dates even
// when the server thinks in UTC.
var colombo = time.FixedZone("Asia/Colombo", 5*3600+1800)

func businessToday(now time.Time) time.Time {
	y, m, d := now.In(colombo).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (s *Server) documentsPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		docType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))
		if !store.ValidDocType(docType) {
			docType = ""
		}
		search := strings.TrimSpace(r.URL.Query().Get("q"))
		readAll := auth.Can(r.Context(), "document.read.all")

		filter := store.DocumentFilter{DocType: docType, Search: search, Limit: 200}
		if !readAll {
			owner := u.User.ID
			filter.CreatedBy = &owner
		}

		var docs []store.Document
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			docs, err = q.ListDocuments(r.Context(), t.ID, filter)
			return err
		})
		if err != nil {
			s.internalError(w, r, "documents", err)
			return
		}

		s.render(w, r, http.StatusOK, views.Documents(views.DocumentsData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			Query:      search,
			Type:       docType,
			CanCreate:  auth.Can(r.Context(), "document.create"),
			ReadAll:    readAll,
			Types:      documentTypeOptions(docType, false, true),
			Rows:       documentRows(docs),
		}))
	}
}

func (s *Server) documentNewPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields := views.DocumentFields{
			DocType: store.DocSalesCash,
			DocDate: businessToday(s.now()).Format("2006-01-02"),
		}
		heading := "New document"
		locked := false
		var quoteNo, customerLabel, vehicleLabel string

		queryType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))
		if store.ValidDocType(queryType) {
			fields.DocType = queryType
		}

		var customerID, vehicleID uuid.UUID
		if id, err := uuid.Parse(r.URL.Query().Get("customer_id")); err == nil {
			customerID = id
			fields.CustomerID = id.String()
		}
		if id, err := uuid.Parse(r.URL.Query().Get("vehicle_id")); err == nil {
			vehicleID = id
			fields.VehicleID = id.String()
		}

		var quote store.Document
		quoteID := r.URL.Query().Get("quote_id")
		if quoteID != "" {
			id, err := uuid.Parse(quoteID)
			if err != nil {
				s.renderError(w, r, http.StatusNotFound, "Quotation not found", "There is nothing at this address.")
				return
			}
			err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
				var err error
				quote, err = q.DocumentByID(r.Context(), t.ID, id)
				return err
			})
			if errors.Is(err, store.ErrNotFound) {
				s.renderError(w, r, http.StatusNotFound, "Quotation not found", "That quotation may have been removed.")
				return
			}
			if err != nil {
				s.internalError(w, r, "quotation", err)
				return
			}
			if quote.DocType != store.DocQuotation || quote.Status != store.DocOpen {
				s.renderError(w, r, http.StatusConflict, "That quotation is closed",
					"It has already been converted, or it is not a quotation.")
				return
			}
			locked = true
			heading = "New invoice from quotation " + quote.DocNo
			fields.DocType = store.DocSalesCash
			fields.CustomerID = quote.CustomerID.String()
			fields.VehicleID = quote.VehicleID.String()
			fields.Price = money.Format(quote.PriceCents)
			fields.Down = money.Format(quote.DownCents)
			fields.Notes = quote.Notes
			fields.QuoteID = quote.ID.String()
			quoteNo = quote.DocNo
			customerLabel = quote.CustomerName
			vehicleLabel = strings.TrimSpace(quote.VehicleMake + " " + quote.VehicleModel + " · " + quote.VehicleChassisNo)
			customerID = quote.CustomerID
			vehicleID = quote.VehicleID
		}

		customers, vehicles, err := s.documentOptions(r.Context(), t.ID)
		if err != nil {
			s.internalError(w, r, "document options", err)
			return
		}

		// Fill in whatever the link already told us, without overriding a quote.
		if !locked {
			if vehicleID != uuid.Nil {
				var v store.Vehicle
				err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
					var err error
					v, err = q.VehicleByID(r.Context(), t.ID, vehicleID)
					return err
				})
				if err == nil {
					if fields.Price == "" {
						fields.Price = money.Format(v.PriceCents)
					}
					vehicleLabel = strings.TrimSpace(v.Make + " " + v.Model + " · " + v.ChassisNo)
				}
			}
			if customerID != uuid.Nil {
				var c store.Customer
				err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
					var err error
					c, err = q.CustomerByID(r.Context(), t.ID, customerID)
					return err
				})
				if err == nil {
					customerLabel = c.Name
				}
			}
		}

		s.render(w, r, http.StatusOK, views.DocumentForm(views.DocumentFormData{
			TenantName:    t.Name,
			UserName:      u.User.Name,
			Nav:           navLinks(r.Context(), r.URL.Path),
			CSRFToken:     csrfToken(r.Context()),
			Action:        "/documents",
			Heading:       heading,
			Submit:        "Issue document",
			Cancel:        "/documents",
			Fields:        fields,
			Customers:     customers,
			Vehicles:      vehicles,
			Types:         documentTypeOptions(fields.DocType, locked, false),
			QuoteLocked:   locked,
			QuoteNo:       quoteNo,
			CustomerLabel: customerLabel,
			VehicleLabel:  vehicleLabel,
			CanApprove:    auth.Can(r.Context(), "document.approve"),
		}))
	}
}

func (s *Server) documentCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields, params, message := readDocumentForm(r, s.now())
		if message != "" {
			s.renderDocumentFormError(w, r, t, u, http.StatusBadRequest, fields, message)
			return
		}
		// The business date the server thinks it is, so the backdate rule
		// compares against the dealership's day rather than UTC.
		params.Today = businessToday(s.now())

		// C8 and C9: a discount past the tenant's percent or a date past the
		// tenant's limit needs a manager's pin. The pin is the manager's own
		// password, so a clerk cannot forge it: the credentials are
		// re-authenticated here against this tenant's users and then checked
		// against the effective document.approve permission.
		pinEmail := strings.ToLower(strings.TrimSpace(r.PostFormValue("approver_email")))
		pinPassword := r.PostFormValue("approver_password")
		pinNote := strings.TrimSpace(r.PostFormValue("approval_note"))
		if pinEmail != "" || pinPassword != "" {
			approver, pinProblem, pinErr := s.authenticateApprover(r, t.ID, pinEmail, pinPassword)
			if pinErr != nil {
				s.internalError(w, r, "approver", pinErr)
				return
			}
			if pinProblem != "" {
				s.renderDocumentApprovalError(w, r, t, u, fields, views.ApprovalPin{
					Email: pinEmail,
					Note:  pinNote,
				}, pinProblem)
				return
			}
			params.ApprovedBy = &approver
			params.ApprovalNote = pinNote
		}

		var res store.DocumentResult
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			res, err = q.CreateDocument(r.Context(), t.ID, u.User.ID, params)
			if err != nil {
				return err
			}
			d := res.Document
			after := map[string]string{
				"type":     d.DocType,
				"number":   d.DocNo,
				"customer": d.CustomerName,
				"vehicle":  d.VehicleChassisNo,
				"price":    money.Format(d.PriceCents),
				"down":     money.Format(d.DownCents),
				"balance":  money.Format(d.BalanceCents),
			}
			// C8 and C9: both people end up on the audit row, so the log
			// says who sold it and who signed the price or date off.
			if d.ApprovedBy != nil {
				after["approved_by"] = d.ApprovedBy.String()
				if d.ApprovalNote != "" {
					after["approval_note"] = d.ApprovalNote
				}
			}
			if err := audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "document.create",
				Entity:   "document",
				EntityID: &d.ID,
				After:    after,
			}); err != nil {
				return err
			}

			if res.StockMoved {
				action := "stock.sell"
				if err := audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
					Action:   action,
					Entity:   "vehicle",
					EntityID: &d.VehicleID,
					Before:   map[string]string{"status": res.VehicleBefore},
					After:    map[string]string{"status": res.VehicleAfter, "document": d.DocNo},
				}); err != nil {
					return err
				}
			} else if res.VehicleAfter == store.StatusReserved && res.VehicleBefore == store.StatusAvailable {
				if err := audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
					Action:   "stock.reserve",
					Entity:   "vehicle",
					EntityID: &d.VehicleID,
					Before:   map[string]string{"status": res.VehicleBefore},
					After:    map[string]string{"status": res.VehicleAfter, "document": d.DocNo},
				}); err != nil {
					return err
				}
			}

			if res.ConvertedQuoteID != nil {
				if err := audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
					Action:   "document.quote_convert",
					Entity:   "document",
					EntityID: res.ConvertedQuoteID,
					After:    map[string]string{"converted_to": d.DocNo},
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNeedsApproval):
				// C8 or C9 tripped. Say which rule and by how much, using
				// the tenant's own numbers, and open the pin so the manager
				// can sign it off without the clerk retyping anything.
				reason, reasonErr := s.approvalReason(r, t.ID, params)
				if reasonErr != nil {
					s.internalError(w, r, "approval reason", reasonErr)
					return
				}
				s.renderDocumentApprovalError(w, r, t, u, fields, views.ApprovalPin{Note: pinNote}, reason)
			case errors.Is(err, store.ErrVehicleUnavailable):
				s.renderDocumentFormError(w, r, t, u, http.StatusConflict, fields,
					"That unit is no longer available. Pick another vehicle.")
			case errors.Is(err, store.ErrQuoteNotOpen):
				s.renderDocumentFormError(w, r, t, u, http.StatusConflict, fields,
					"That quotation has already been used, or it belongs to a different customer or vehicle.")
			case errors.Is(err, store.ErrInvalidDocument):
				s.renderDocumentFormError(w, r, t, u, http.StatusBadRequest, fields,
					"Check the amounts and the document details.")
			case errors.Is(err, store.ErrNotFound):
				s.renderDocumentFormError(w, r, t, u, http.StatusBadRequest, fields,
					"The customer or vehicle no longer exists.")
			default:
				s.internalError(w, r, "document create", err)
			}
			return
		}

		http.Redirect(w, r, "/documents/"+res.Document.ID.String()+"?created=1", http.StatusSeeOther)
	}
}

func (s *Server) documentPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Document not found", "There is nothing at this address.")
			return
		}

		var d store.Document
		var balance store.DocumentBalance
		var receipts []store.Payment
		var approverName, voiderName string
		isInvoice := false
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			d, err = q.DocumentByID(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			isInvoice = d.DocType == store.DocSalesCash || d.DocType == store.DocSalesLease
			// The approval and the void each name a person, so the page can
			// say who rather than show an id.
			if d.ApprovedBy != nil {
				if approver, err := q.UserByID(r.Context(), t.ID, *d.ApprovedBy); err == nil {
					approverName = approver.Name
				}
			}
			if d.VoidedBy != nil {
				if voidedBy, err := q.UserByID(r.Context(), t.ID, *d.VoidedBy); err == nil {
					voiderName = voidedBy.Name
				}
			}
			if !isInvoice {
				return nil
			}
			// What is owed is derived from the receipts, so the invoice paper
			// and the money screen can never disagree.
			balance, err = q.DocumentBalanceFor(r.Context(), t.ID, id)
			return err
		})
		if errors.Is(err, store.ErrNotFound) || (err == nil && !canReadDocument(r.Context(), d, u.User.ID)) {
			s.renderError(w, r, http.StatusNotFound, "Document not found", "That document may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "document", err)
			return
		}

		canSeePayments := isInvoice && auth.Can(r.Context(), "payment.read")
		if canSeePayments {
			err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
				var err error
				receipts, err = q.ListPayments(r.Context(), t.ID, store.PaymentFilter{DocumentID: &d.ID, Limit: 100})
				return err
			})
			if err != nil {
				s.internalError(w, r, "document receipts", err)
				return
			}
		}

		s.renderDocumentDetail(w, r, t, u, d, isInvoice, balance, receipts, approverName, voiderName, "")
	}
}

// renderDocumentDetail draws the one document screen. It is shared by the
// ordinary read and by a void that was refused, so the page the person gets
// back after a refusal is the same page with the reason on it.
func (s *Server) renderDocumentDetail(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, d store.Document, isInvoice bool, balance store.DocumentBalance, receipts []store.Payment, approverName, voiderName, voidError string) {
	s.render(w, r, http.StatusOK, views.DocumentDetail(views.DocumentDetailData{
		TenantName:      t.Name,
		UserName:        u.User.Name,
		Nav:             navLinks(r.Context(), r.URL.Path),
		ID:              d.ID.String(),
		TypeLabel:       documentTypeLabel(d.DocType),
		DocNo:           d.DocNo,
		Date:            d.DocDate.Format("02/01/2006"),
		Status:          d.Status,
		Created:         r.URL.Query().Get("created") == "1",
		CustomerID:      d.CustomerID.String(),
		CustomerName:    d.CustomerName,
		CustomerNIC:     d.CustomerNIC,
		CustomerPhone:   d.CustomerPhone,
		CustomerAddress: d.CustomerAddress,
		VehicleID:       d.VehicleID.String(),
		VehicleLine:     strings.TrimSpace(d.VehicleMake + " " + d.VehicleModel),
		Chassis:         d.VehicleChassisNo,
		Engine:          d.VehicleEngineNo,
		Colour:          d.VehicleColour,
		Type:            d.VehicleType,
		YOM:             documentYOM(d.VehicleYOM),
		Price:           money.Format(d.PriceCents),
		Down:            money.Format(d.DownCents),
		Balance:         money.Format(d.BalanceCents),
		FinanceCompany:  d.FinanceCompany,
		FinanceAddress:  d.FinanceAddress,
		Notes:           d.Notes,
		CanCreate:       auth.Can(r.Context(), "document.create"),
		CanPrint:        s.pdf != nil,
		IsQuote:         d.DocType == store.DocQuotation,
		IsOpenQuote:     d.DocType == store.DocQuotation && d.Status == store.DocOpen,
		IsInvoice:       isInvoice,
		CanSeePayments:  isInvoice && auth.Can(r.Context(), "payment.read"),
		CanTakePayment:  isInvoice && d.Status != store.DocVoid && balance.OutstandingCents > 0 && auth.Can(r.Context(), "payment.record"),
		Paid:            money.Format(balance.PaidCents),
		Outstanding:     money.Format(balance.OutstandingCents),
		Receipts:        receiptRows(receipts),
		CSRFToken:       csrfToken(r.Context()),
		CanVoid:         d.Status != store.DocVoid && auth.Can(r.Context(), "document.void"),
		Voided:          d.Status == store.DocVoid,
		VoidName:        voiderName,
		VoidedAt:        documentTimestamp(d.VoidedAt),
		VoidReason:      d.VoidReason,
		VoidError:       voidError,
		ApprovalName:    approverName,
		ApprovalAt:      documentTimestamp(d.ApprovedAt),
		ApprovalNote:    d.ApprovalNote,
	}))
}

// documentTimestamp renders one of the two moments an issued document can gain
// (an approval, a void), or nothing when it has not happened.
func documentTimestamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(colombo).Format("02/01/2006 15:04")
}

// documentVoid is the only way a document leaves the ledger (C4). It keeps the
// paper and adds who, when and why, reverses the stock the document moved, and
// records the alert in the same transaction as the void itself, so a crash
// cannot leave an unexplained void with nobody told.
func (s *Server) documentVoid() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Document not found", "There is nothing at this address.")
			return
		}
		reason := strings.TrimSpace(r.PostFormValue("reason"))

		var d store.Document
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			d, err = q.VoidDocument(r.Context(), t.ID, id, u.User.ID, reason, businessToday(s.now()))
			if err != nil {
				return err
			}
			if err := audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "document.void",
				Entity:   "document",
				EntityID: &d.ID,
				Before:   map[string]string{"status": store.DocIssued},
				After: map[string]string{
					"status": d.Status,
					"reason": d.VoidReason,
					"doc_no": d.DocNo,
				},
			}); err != nil {
				return err
			}
			// C12: a void is always told to somebody. The alert is written
			// inside this transaction, so the record and the message cannot
			// disagree.
			_, err = q.RecordAlert(r.Context(), t.ID, store.AlertParams{
				Kind:     store.AlertVoid,
				Message:  documentTypeLabel(d.DocType) + " " + d.DocNo + " was voided by " + u.User.Name + ": " + d.VoidReason,
				Severity: store.SeverityWarning,
				Entity:   d.DocNo,
			})
			return err
		})
		switch {
		case err == nil:
			// The alert is already on the record from inside the transaction;
			// this is only the push out of the building, and a failure here
			// leaves it queued for the next attempt.
			s.deliverAlerts(t.ID)
			http.Redirect(w, r, "/documents/"+id.String()+"?voided=1", http.StatusSeeOther)
		case errors.Is(err, store.ErrInvalidDocument):
			s.voidFormBack(w, r, t, u, id, "Give a reason before voiding. A void cannot be undone, and a converted quotation cannot be voided on its own.")
		case errors.Is(err, store.ErrDayAlreadyClosed):
			s.voidFormBack(w, r, t, u, id, "This day is already closed. A void is dated the day it happens, so it cannot land on a closed z.")
		case errors.Is(err, store.ErrNotFound):
			s.renderError(w, r, http.StatusNotFound, "Document not found", "That document may have been removed.")
		default:
			s.internalError(w, r, "document void", err)
		}
	}
}

// voidFormBack puts the document page up again with the refusal on it and the
// reason the person typed still in the box.
func (s *Server) voidFormBack(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, id uuid.UUID, message string) {
	var d store.Document
	var balance store.DocumentBalance
	var receipts []store.Payment
	var approverName, voiderName string
	err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
		var err error
		d, err = q.DocumentByID(r.Context(), t.ID, id)
		return err
	})
	if err != nil {
		s.internalError(w, r, "document", err)
		return
	}
	isInvoice := d.DocType == store.DocSalesCash || d.DocType == store.DocSalesLease
	if isInvoice {
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			balance, err = q.DocumentBalanceFor(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			receipts, err = q.ListPayments(r.Context(), t.ID, store.PaymentFilter{DocumentID: &id, Limit: 100})
			return err
		})
		if err != nil {
			s.internalError(w, r, "document receipts", err)
			return
		}
	}
	s.renderDocumentDetail(w, r, t, u, d, isInvoice, balance, receipts, approverName, voiderName, message)
}

func (s *Server) documentPDF() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Document not found", "There is nothing at this address.")
			return
		}

		var d store.Document
		var archived store.DocumentPDF
		var found bool
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			d, err = q.DocumentByID(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			archived, err = q.DocumentPDFByID(r.Context(), t.ID, id)
			switch {
			case err == nil:
				found = true
				return nil
			case errors.Is(err, store.ErrNotFound):
				return nil
			default:
				return err
			}
		})
		if errors.Is(err, store.ErrNotFound) || (err == nil && !canReadDocument(r.Context(), d, u.User.ID)) {
			s.renderError(w, r, http.StatusNotFound, "Document not found", "That document may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "document pdf", err)
			return
		}

		if s.pdf == nil {
			s.renderError(w, r, http.StatusServiceUnavailable, "Printing is not available",
				"This deployment has no PDF renderer configured.")
			return
		}

		if !found {
			bytes, err := s.pdf.Render(r.Context(), t.Name, d)
			if err != nil {
				s.log.Error("pdf render failed",
					"error", err,
					"document", d.ID,
					"request_id", httpx.RequestID(r.Context()),
				)
				s.renderError(w, r, http.StatusBadGateway, "The PDF could not be generated",
					"The document is safe. Try printing again in a moment.")
				return
			}

			err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
				stored, created, err := q.SaveDocumentPDF(r.Context(), t.ID, d.ID, u.User.ID, bytes)
				if err != nil {
					return err
				}
				archived = stored
				if created {
					return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
						Action:   "document.pdf",
						Entity:   "document",
						EntityID: &d.ID,
						After:    map[string]string{"sha256": stored.SHA256},
					})
				}
				return nil
			})
			if errors.Is(err, store.ErrNotFound) {
				s.renderError(w, r, http.StatusNotFound, "Document not found", "That document may have been removed.")
				return
			}
			if err != nil {
				s.internalError(w, r, "archive document pdf", err)
				return
			}
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `inline; filename="`+pdf.Filename(d)+`"`)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archived.Bytes)))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, err := w.Write(archived.Bytes); err != nil {
			s.log.Warn("send pdf", "error", err, "document", d.ID)
		}
	}
}

func canReadDocument(ctx context.Context, d store.Document, userID uuid.UUID) bool {
	if auth.Can(ctx, "document.read.all") {
		return true
	}
	return d.CreatedBy != nil && *d.CreatedBy == userID
}

func (s *Server) documentOptions(ctx context.Context, tenantID uuid.UUID) ([]views.CustomerOption, []views.VehicleOption, error) {
	var customers []store.Customer
	var vehicles []store.Vehicle
	err := s.store.InTenant(ctx, tenantID, func(q *store.Store) error {
		var err error
		customers, err = q.ListCustomers(ctx, tenantID, "", 1000)
		if err != nil {
			return err
		}
		vehicles, err = q.ListVehicles(ctx, tenantID, "", "", 500)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	customerOptions := make([]views.CustomerOption, 0, len(customers))
	for _, c := range customers {
		label := c.Name
		if c.NIC != "" {
			label += " · " + c.NIC
		}
		customerOptions = append(customerOptions, views.CustomerOption{ID: c.ID.String(), Label: label})
	}

	vehicleOptions := make([]views.VehicleOption, 0, len(vehicles))
	for _, v := range vehicles {
		if v.Status != store.StatusAvailable && v.Status != store.StatusReserved {
			continue
		}
		label := strings.TrimSpace(v.Make + " " + v.Model)
		if v.StockNo != "" {
			label = v.StockNo + " · " + label
		}
		label += " · " + v.ChassisNo + " · Rs " + money.Format(v.PriceCents)
		vehicleOptions = append(vehicleOptions, views.VehicleOption{
			ID:    v.ID.String(),
			Label: label,
			Price: money.Format(v.PriceCents),
		})
	}
	return customerOptions, vehicleOptions, nil
}

func (s *Server) renderDocumentFormError(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, status int, fields views.DocumentFields, message string) {
	customers, vehicles, err := s.documentOptions(r.Context(), t.ID)
	if err != nil {
		s.internalError(w, r, "document options", err)
		return
	}
	locked := fields.QuoteID != ""
	customerLabel, vehicleLabel, quoteNo := "", "", ""
	if locked {
		for _, c := range customers {
			if c.ID == fields.CustomerID {
				customerLabel = c.Label
			}
		}
		for _, v := range vehicles {
			if v.ID == fields.VehicleID {
				vehicleLabel = v.Label
			}
		}
		if id, err := uuid.Parse(fields.QuoteID); err == nil {
			_ = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
				quote, err := q.DocumentByID(r.Context(), t.ID, id)
				if err == nil {
					quoteNo = quote.DocNo
				}
				return nil
			})
		}
	}

	s.render(w, r, status, views.DocumentForm(views.DocumentFormData{
		TenantName:    t.Name,
		UserName:      u.User.Name,
		Nav:           navLinks(r.Context(), r.URL.Path),
		CSRFToken:     csrfToken(r.Context()),
		Action:        "/documents",
		Heading:       "New document",
		Submit:        "Issue document",
		Cancel:        "/documents",
		Fields:        fields,
		Customers:     customers,
		Vehicles:      vehicles,
		Types:         documentTypeOptions(fields.DocType, locked, false),
		QuoteLocked:   locked,
		QuoteNo:       quoteNo,
		CustomerLabel: customerLabel,
		VehicleLabel:  vehicleLabel,
		Error:         message,
		CanApprove:    auth.Can(r.Context(), "document.approve"),
	}))
}

// authenticateApprover re-checks a manager's pin. The pin is the manager's own
// password, so the clerk at the counter cannot forge an approval: the
// credentials are verified against this tenant's users and then checked
// against the effective document.approve permission, which a per-user override
// may have taken away from the role. The second return is the sentence the
// person at the screen reads, or empty when the pin is good.
func (s *Server) authenticateApprover(r *http.Request, tenantID uuid.UUID, email, password string) (uuid.UUID, string, error) {
	if email == "" || password == "" {
		return uuid.Nil, "Enter the manager's email and password, or leave both blank.", nil
	}
	var (
		approver uuid.UUID
		problem  string
	)
	err := s.store.InTenant(r.Context(), tenantID, func(q *store.Store) error {
		u, err := q.UserByEmail(r.Context(), tenantID, email)
		if err != nil {
			return err
		}
		if !u.IsActive {
			problem = "That manager's account is not active."
			return nil
		}
		ok, err := auth.VerifyPassword(u.PasswordHash, password)
		if err != nil || !ok {
			problem = "That manager's password did not match."
			return nil
		}
		can, err := q.ApproverCan(r.Context(), tenantID, u.ID)
		if err != nil {
			return err
		}
		if !can {
			problem = "That user is not allowed to approve documents."
			return nil
		}
		approver = u.ID
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return uuid.Nil, "No active user with that email in this dealership.", nil
		}
		return uuid.Nil, "", err
	}
	return approver, problem, nil
}

// approvalReason turns the rule that tripped into the sentence the counter
// reads, in the tenant's own numbers rather than a generic warning.
func (s *Server) approvalReason(r *http.Request, tenantID uuid.UUID, p store.DocumentParams) (string, error) {
	var reason store.ApprovalReason
	var needed bool
	err := s.store.InTenant(r.Context(), tenantID, func(q *store.Store) error {
		var err error
		reason, needed, err = q.ApprovalNeeded(r.Context(), tenantID, p.DocType, p.PriceCents, p.VehicleID, p.DocDate, p.Today)
		return err
	})
	if err != nil {
		return "", err
	}
	if !needed {
		// The store refused for a reason this handler has not seen; say
		// something honest rather than inventing a rule.
		return "This document needs a manager to approve it.", nil
	}
	switch reason.Kind {
	case "DISCOUNT":
		return fmt.Sprintf(
			"This price is %d%% below the listed price. The limit without approval is %d%%, so a manager has to sign it off.",
			reason.Percent, reason.Limit), nil
	case "BACKDATE":
		return fmt.Sprintf(
			"This document is dated %d days back. The limit without approval is %d days, so a manager has to sign it off.",
			reason.Days, reason.LimitD), nil
	}
	return "This document needs a manager to approve it.", nil
}

// renderDocumentApprovalError puts the form back on screen with the pin block
// open, the reason it is needed, and whatever the clerk had already typed
// (never the password).
func (s *Server) renderDocumentApprovalError(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, fields views.DocumentFields, pin views.ApprovalPin, message string) {
	customers, vehicles, err := s.documentOptions(r.Context(), t.ID)
	if err != nil {
		s.internalError(w, r, "document options", err)
		return
	}
	s.render(w, r, http.StatusBadRequest, views.DocumentForm(views.DocumentFormData{
		TenantName:     t.Name,
		UserName:       u.User.Name,
		Nav:            navLinks(r.Context(), r.URL.Path),
		CSRFToken:      csrfToken(r.Context()),
		Action:         "/documents",
		Heading:        "New document",
		Submit:         "Issue document",
		Cancel:         "/documents",
		Fields:         fields,
		Customers:      customers,
		Vehicles:       vehicles,
		Types:          documentTypeOptions(fields.DocType, fields.QuoteID != "", false),
		QuoteLocked:    fields.QuoteID != "",
		ApprovalNeeded: true,
		ApprovalReason: message,
		Approval:       pin,
		CanApprove:     auth.Can(r.Context(), "document.approve"),
	}))
}

func readDocumentForm(r *http.Request, now time.Time) (views.DocumentFields, store.DocumentParams, string) {
	f := views.DocumentFields{
		DocType:        strings.ToUpper(strings.TrimSpace(r.PostFormValue("doc_type"))),
		DocDate:        strings.TrimSpace(r.PostFormValue("doc_date")),
		CustomerID:     strings.TrimSpace(r.PostFormValue("customer_id")),
		VehicleID:      strings.TrimSpace(r.PostFormValue("vehicle_id")),
		Price:          strings.TrimSpace(r.PostFormValue("price")),
		Down:           strings.TrimSpace(r.PostFormValue("down")),
		FinanceCompany: strings.TrimSpace(r.PostFormValue("finance_company")),
		FinanceAddress: strings.TrimSpace(r.PostFormValue("finance_address")),
		Notes:          strings.TrimSpace(r.PostFormValue("notes")),
		QuoteID:        strings.TrimSpace(r.PostFormValue("quote_id")),
	}
	fail := func(message string) (views.DocumentFields, store.DocumentParams, string) {
		return f, store.DocumentParams{}, message
	}

	if !store.ValidDocType(f.DocType) {
		return fail("Choose what kind of document this is.")
	}
	if f.QuoteID != "" && f.DocType != store.DocSalesCash && f.DocType != store.DocSalesLease {
		return fail("A quotation can only become an invoice.")
	}

	customerID, err := uuid.Parse(f.CustomerID)
	if err != nil {
		return fail("Choose a customer.")
	}
	vehicleID, err := uuid.Parse(f.VehicleID)
	if err != nil {
		return fail("Choose a vehicle.")
	}

	docDate, err := time.Parse("2006-01-02", f.DocDate)
	if err != nil {
		return fail("The date must look like 2026-10-30.")
	}
	if docDate.After(businessToday(now)) {
		return fail("The document date cannot be in the future.")
	}

	price, err := money.Parse(f.Price)
	if err != nil {
		return fail("Price: " + err.Error() + ".")
	}
	down := int64(0)
	if f.Down != "" {
		down, err = money.Parse(f.Down)
		if err != nil {
			return fail("Advance: " + err.Error() + ".")
		}
	}
	if utf8.RuneCountInString(f.FinanceCompany) > 120 {
		return fail("The finance company name is too long. 120 characters at most.")
	}
	if utf8.RuneCountInString(f.FinanceAddress) > 500 {
		return fail("The finance company address is too long. 500 characters at most.")
	}
	if utf8.RuneCountInString(f.Notes) > 2000 {
		return fail("The notes are too long. 2000 characters at most.")
	}
	if f.DocType == store.DocSalesLease || f.DocType == store.DocProforma {
		if f.FinanceCompany == "" {
			return fail("Enter the finance company.")
		}
	}
	if f.DocType == store.DocProforma && !strings.Contains(strings.ToUpper(f.FinanceCompany), "SINGER") {
		return fail("A proforma is printed on Singer Finance paper. Other leasing companies arrive with settings.")
	}

	p := store.DocumentParams{
		DocType:        f.DocType,
		DocDate:        docDate,
		CustomerID:     customerID,
		VehicleID:      vehicleID,
		FinanceCompany: f.FinanceCompany,
		FinanceAddress: f.FinanceAddress,
		PriceCents:     price,
		DownCents:      down,
		Notes:          f.Notes,
	}
	if f.QuoteID != "" {
		id, err := uuid.Parse(f.QuoteID)
		if err != nil {
			return fail("The quotation reference is not valid.")
		}
		p.QuoteID = &id
	}
	return f, p, ""
}

func documentRows(docs []store.Document) []views.DocumentRow {
	rows := make([]views.DocumentRow, 0, len(docs))
	for _, d := range docs {
		rows = append(rows, views.DocumentRow{
			ID:       d.ID.String(),
			No:       d.DocNo,
			Type:     documentTypeLabel(d.DocType),
			Date:     d.DocDate.Format("02/01/2006"),
			Customer: d.CustomerName,
			Vehicle:  strings.TrimSpace(d.VehicleMake + " " + d.VehicleModel),
			Amount:   money.Format(d.PriceCents),
			Status:   d.Status,
		})
	}
	return rows
}

func documentTypeLabel(docType string) string {
	switch docType {
	case store.DocQuotation:
		return "Quotation"
	case store.DocSalesCash:
		return "Cash sale"
	case store.DocSalesLease:
		return "Leasing sale"
	case store.DocProforma:
		return "Proforma"
	case store.DocAdvance:
		return "Advance receipt"
	}
	return docType
}

func documentTypeOptions(selected string, forQuote, includeAll bool) []views.DocTypeOption {
	types := []struct {
		value string
		label string
	}{
		{store.DocQuotation, "Quotation"},
		{store.DocSalesCash, "Cash sale"},
		{store.DocSalesLease, "Leasing sale"},
		{store.DocProforma, "Proforma"},
		{store.DocAdvance, "Advance receipt"},
	}
	if forQuote {
		types = []struct {
			value string
			label string
		}{
			{store.DocSalesCash, "Cash sale"},
			{store.DocSalesLease, "Leasing sale"},
		}
	}

	out := make([]views.DocTypeOption, 0, len(types)+1)
	if includeAll {
		out = append(out, views.DocTypeOption{Value: "", Label: "All types", Selected: selected == ""})
	}
	for _, t := range types {
		out = append(out, views.DocTypeOption{Value: t.value, Label: t.label, Selected: t.value == selected})
	}
	return out
}

func documentYOM(yom *int32) string {
	if yom == nil {
		return ""
	}
	return fmt.Sprintf("%d", *yom)
}
