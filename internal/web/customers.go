package web

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) customersPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		search := strings.TrimSpace(r.URL.Query().Get("q"))

		var customers []store.Customer
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			customers, err = q.ListCustomers(r.Context(), t.ID, search, 100)
			return err
		})
		if err != nil {
			s.internalError(w, r, "customers", err)
			return
		}

		rows := make([]views.CustomerRow, 0, len(customers))
		for _, c := range customers {
			rows = append(rows, views.CustomerRow{
				ID:      c.ID.String(),
				Name:    c.Name,
				NIC:     c.NIC,
				Phone:   c.Phone,
				Address: c.Address,
			})
		}

		s.render(w, r, http.StatusOK, views.Customers(views.CustomersData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			Query:      search,
			CanWrite:   auth.Can(r.Context(), "customer.write"),
			Rows:       rows,
		}))
	}
}

func (s *Server) customerNewPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		s.render(w, r, http.StatusOK, views.CustomerForm(views.CustomerFormData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			Action:     "/customers",
			Heading:    "Add customer",
			Submit:     "Add customer",
			Cancel:     "/customers",
		}))
	}
}

func (s *Server) customerCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields, message := readCustomerFields(r)
		if message != "" {
			s.renderCustomerForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, views.CustomerFormData{
				Action:  "/customers",
				Heading: "Add customer",
				Submit:  "Add customer",
				Cancel:  "/customers",
				Fields:  fields,
				Error:   message,
			})
			return
		}

		var created store.Customer
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			c, err := q.CreateCustomer(r.Context(), t.ID, u.User.ID, customerParams(fields))
			if err != nil {
				return err
			}
			created = c
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "customer.create",
				Entity:   "customer",
				EntityID: &c.ID,
				After:    customerAudit(c),
			})
		})
		if errors.Is(err, store.ErrDuplicateNIC) {
			s.renderCustomerForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, views.CustomerFormData{
				Action:  "/customers",
				Heading: "Add customer",
				Submit:  "Add customer",
				Cancel:  "/customers",
				Fields:  fields,
				Error:   "A customer with this NIC already exists. Open that customer instead of adding a second one.",
			})
			return
		}
		if err != nil {
			s.internalError(w, r, "customer create", err)
			return
		}

		http.Redirect(w, r, "/customers/"+created.ID.String()+"?saved=1", http.StatusSeeOther)
	}
}

func (s *Server) customerPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "There is nothing at this address.")
			return
		}

		var c store.Customer
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			c, err = q.CustomerByID(r.Context(), t.ID, id)
			return err
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "This customer may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "customer detail", err)
			return
		}

		s.render(w, r, http.StatusOK, views.CustomerDetail(views.CustomerDetailData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			ID:         c.ID.String(),
			Fields:     customerFields(c),
			Saved:      r.URL.Query().Get("saved") == "1",
			CanWrite:   auth.Can(r.Context(), "customer.write"),
			Created:    c.CreatedAt.UTC().Format("2 Jan 2006"),
		}))
	}
}

func (s *Server) customerEditPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "There is nothing at this address.")
			return
		}

		var c store.Customer
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			c, err = q.CustomerByID(r.Context(), t.ID, id)
			return err
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "This customer may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "customer edit", err)
			return
		}

		s.render(w, r, http.StatusOK, views.CustomerForm(views.CustomerFormData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			Action:     "/customers/" + c.ID.String(),
			Heading:    "Edit customer",
			Submit:     "Save changes",
			Cancel:     "/customers/" + c.ID.String(),
			Fields:     customerFields(c),
		}))
	}
}

func (s *Server) customerUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "There is nothing at this address.")
			return
		}

		fields, message := readCustomerFields(r)
		formData := views.CustomerFormData{
			Action:  "/customers/" + id.String(),
			Heading: "Edit customer",
			Submit:  "Save changes",
			Cancel:  "/customers/" + id.String(),
			Fields:  fields,
			Error:   message,
		}
		if message != "" {
			s.renderCustomerForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, formData)
			return
		}

		var before, after store.Customer
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			before, err = q.CustomerByID(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			after, err = q.UpdateCustomer(r.Context(), t.ID, id, customerParams(fields))
			if err != nil {
				return err
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "customer.update",
				Entity:   "customer",
				EntityID: &after.ID,
				Before:   customerAudit(before),
				After:    customerAudit(after),
			})
		})
		if errors.Is(err, store.ErrDuplicateNIC) {
			formData.Error = "Another customer already uses this NIC."
			s.renderCustomerForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, formData)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "This customer may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "customer update", err)
			return
		}

		http.Redirect(w, r, "/customers/"+after.ID.String()+"?saved=1", http.StatusSeeOther)
	}
}

func (s *Server) customerDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "There is nothing at this address.")
			return
		}

		reason := strings.TrimSpace(r.PostFormValue("reason"))
		if reason == "" {
			reason = "removed from the customer list"
		}
		if utf8.RuneCountInString(reason) > 200 {
			reason = string([]rune(reason)[:200])
		}

		var before store.Customer
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			before, err = q.CustomerByID(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			if err := q.SoftDeleteCustomer(r.Context(), t.ID, id); err != nil {
				return err
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "customer.delete",
				Entity:   "customer",
				EntityID: &id,
				Before:   customerAudit(before),
				After:    map[string]string{"reason": reason},
			})
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Customer not found", "This customer may already have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "customer delete", err)
			return
		}

		http.Redirect(w, r, "/customers", http.StatusSeeOther)
	}
}

func (s *Server) renderCustomerForm(w http.ResponseWriter, r *http.Request, tenantName, userName string, status int, d views.CustomerFormData) {
	d.TenantName = tenantName
	d.UserName = userName
	d.Nav = navLinks(r.Context(), r.URL.Path)
	d.CSRFToken = csrfToken(r.Context())
	s.render(w, r, status, views.CustomerForm(d))
}

// readCustomerFields trims and validates what a person typed. The returned
// message is shown as is, so it has to read like a sentence.
func readCustomerFields(r *http.Request) (views.CustomerFields, string) {
	f := views.CustomerFields{
		Name:            strings.TrimSpace(r.PostFormValue("name")),
		NIC:             strings.ToUpper(strings.TrimSpace(r.PostFormValue("nic"))),
		Phone:           strings.TrimSpace(r.PostFormValue("phone")),
		Phone2:          strings.TrimSpace(r.PostFormValue("phone2")),
		Email:           strings.ToLower(strings.TrimSpace(r.PostFormValue("email"))),
		Address:         strings.TrimSpace(r.PostFormValue("address")),
		DeliveryAddress: strings.TrimSpace(r.PostFormValue("delivery_address")),
		Notes:           strings.TrimSpace(r.PostFormValue("notes")),
	}

	switch {
	case f.Name == "":
		return f, "Enter the customer's name."
	case utf8.RuneCountInString(f.Name) > 120:
		return f, "The name is too long. 120 characters at most."
	case utf8.RuneCountInString(f.NIC) > 20:
		return f, "The NIC is too long. 20 characters at most."
	case utf8.RuneCountInString(f.Phone) > 20, utf8.RuneCountInString(f.Phone2) > 20:
		return f, "A phone number is too long. 20 characters at most."
	case utf8.RuneCountInString(f.Address) > 500, utf8.RuneCountInString(f.DeliveryAddress) > 500:
		return f, "An address is too long. 500 characters at most."
	case utf8.RuneCountInString(f.Notes) > 2000:
		return f, "The notes are too long. 2000 characters at most."
	}

	if f.Email != "" {
		addr, err := mail.ParseAddress(f.Email)
		if err != nil || addr.Address != f.Email {
			return f, "Enter a valid email address, or leave it blank."
		}
	}
	return f, ""
}

func customerParams(f views.CustomerFields) store.CustomerParams {
	return store.CustomerParams{
		Name:            f.Name,
		NIC:             f.NIC,
		Phone:           f.Phone,
		Phone2:          f.Phone2,
		Email:           f.Email,
		Address:         f.Address,
		DeliveryAddress: f.DeliveryAddress,
		Notes:           f.Notes,
	}
}

func customerFields(c store.Customer) views.CustomerFields {
	return views.CustomerFields{
		Name:            c.Name,
		NIC:             c.NIC,
		Phone:           c.Phone,
		Phone2:          c.Phone2,
		Email:           c.Email,
		Address:         c.Address,
		DeliveryAddress: c.DeliveryAddress,
		Notes:           c.Notes,
	}
}

func customerAudit(c store.Customer) map[string]string {
	return map[string]string{
		"name":  c.Name,
		"nic":   c.NIC,
		"phone": c.Phone,
		"email": c.Email,
	}
}
