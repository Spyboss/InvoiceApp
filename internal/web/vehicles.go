package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

func (s *Server) vehiclesPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
		switch status {
		case store.StatusAvailable, store.StatusReserved:
		default:
			status = ""
		}
		search := strings.TrimSpace(r.URL.Query().Get("q"))

		var vehicles []store.Vehicle
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			vehicles, err = q.ListVehicles(r.Context(), t.ID, status, search, 100)
			return err
		})
		if err != nil {
			s.internalError(w, r, "vehicles", err)
			return
		}

		s.render(w, r, http.StatusOK, views.Vehicles(views.VehiclesData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			Query:      search,
			Status:     status,
			CanWrite:   auth.Can(r.Context(), "vehicle.write"),
			Rows:       vehicleRows(vehicles),
		}))
	}
}

func (s *Server) vehicleNewPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		s.render(w, r, http.StatusOK, views.VehicleForm(views.VehicleFormData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			Action:     "/vehicles",
			Heading:    "Add vehicle",
			Submit:     "Add vehicle",
			Cancel:     "/vehicles",
			Fields: views.VehicleFields{
				Make:       "Piaggio",
				ReceivedAt: s.now().UTC().Format("2006-01-02"),
			},
		}))
	}
}

func (s *Server) vehicleCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields, params, message := readVehicleForm(r, s.now())
		if message != "" {
			s.renderVehicleForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, views.VehicleFormData{
				Action:  "/vehicles",
				Heading: "Add vehicle",
				Submit:  "Add vehicle",
				Cancel:  "/vehicles",
				Fields:  fields,
				Error:   message,
			})
			return
		}

		var created store.Vehicle
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			v, err := q.CreateVehicle(r.Context(), t.ID, u.User.ID, params)
			if err != nil {
				return err
			}
			created = v
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "vehicle.create",
				Entity:   "vehicle",
				EntityID: &v.ID,
				After:    vehicleAudit(v),
			})
		})
		if errors.Is(err, store.ErrDuplicateChassis) || errors.Is(err, store.ErrDuplicateStockNo) {
			s.renderVehicleForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, views.VehicleFormData{
				Action:  "/vehicles",
				Heading: "Add vehicle",
				Submit:  "Add vehicle",
				Cancel:  "/vehicles",
				Fields:  fields,
				Error:   vehicleDuplicateMessage(err),
			})
			return
		}
		if err != nil {
			s.internalError(w, r, "vehicle create", err)
			return
		}

		http.Redirect(w, r, "/vehicles/"+created.ID.String()+"?saved=1", http.StatusSeeOther)
	}
}

func (s *Server) vehiclePage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "There is nothing at this address.")
			return
		}

		var v store.Vehicle
		var movements []store.StockMovement
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			v, err = q.VehicleByID(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			movements, err = q.MovementsForVehicle(r.Context(), t.ID, id, 50)
			return err
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "This unit may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "vehicle detail", err)
			return
		}

		canWrite := auth.Can(r.Context(), "vehicle.write")
		s.render(w, r, http.StatusOK, views.VehicleDetail(views.VehicleDetailData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			ID:         v.ID.String(),
			Fields:     vehicleFields(v),
			Status:     v.Status,
			Saved:      r.URL.Query().Get("saved") == "1",
			CanWrite:   canWrite,
			CanMove:    auth.Can(r.Context(), "stock.write"),
			Movements:  movementRows(movements),
		}))
	}
}

func (s *Server) vehicleEditPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "There is nothing at this address.")
			return
		}

		var v store.Vehicle
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			v, err = q.VehicleByID(r.Context(), t.ID, id)
			return err
		})
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "This unit may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "vehicle edit", err)
			return
		}

		s.render(w, r, http.StatusOK, views.VehicleForm(views.VehicleFormData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			Action:     "/vehicles/" + v.ID.String(),
			Heading:    "Edit vehicle",
			Submit:     "Save changes",
			Cancel:     "/vehicles/" + v.ID.String(),
			Fields:     vehicleFields(v),
		}))
	}
}

func (s *Server) vehicleUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "There is nothing at this address.")
			return
		}

		fields, params, message := readVehicleForm(r, s.now())
		formData := views.VehicleFormData{
			Action:  "/vehicles/" + id.String(),
			Heading: "Edit vehicle",
			Submit:  "Save changes",
			Cancel:  "/vehicles/" + id.String(),
			Fields:  fields,
			Error:   message,
		}
		if message != "" {
			s.renderVehicleForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, formData)
			return
		}

		var before, after store.Vehicle
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			before, err = q.VehicleByID(r.Context(), t.ID, id)
			if err != nil {
				return err
			}
			after, err = q.UpdateVehicle(r.Context(), t.ID, id, params)
			if err != nil {
				return err
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "vehicle.update",
				Entity:   "vehicle",
				EntityID: &after.ID,
				Before:   vehicleAudit(before),
				After:    vehicleAudit(after),
			})
		})
		if errors.Is(err, store.ErrDuplicateChassis) || errors.Is(err, store.ErrDuplicateStockNo) {
			formData.Error = vehicleDuplicateMessage(err)
			s.renderVehicleForm(w, r, t.Name, u.User.Name, http.StatusBadRequest, formData)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "This unit may have been removed.")
			return
		}
		if err != nil {
			s.internalError(w, r, "vehicle update", err)
			return
		}

		http.Redirect(w, r, "/vehicles/"+after.ID.String()+"?saved=1", http.StatusSeeOther)
	}
}

// vehicleStatusSubmit reserves or releases one unit. Reserve and release are
// audited status changes, not ledger rows: the unit never left the yard.
func (s *Server) vehicleStatusSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "There is nothing at this address.")
			return
		}

		to := r.PostFormValue("status")
		if to != store.StatusReserved && to != store.StatusAvailable {
			s.renderError(w, r, http.StatusBadRequest, "Unknown action", "That status change is not available.")
			return
		}

		note := strings.TrimSpace(r.PostFormValue("note"))
		if utf8.RuneCountInString(note) > 200 {
			note = string([]rune(note)[:200])
		}

		var after store.Vehicle
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			before, next, err := q.TransitionVehicle(r.Context(), t.ID, id, to)
			if err != nil {
				return err
			}
			after = next

			action := "stock.reserve"
			if to == store.StatusAvailable {
				action = "stock.release"
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   action,
				Entity:   "vehicle",
				EntityID: &next.ID,
				Before:   map[string]string{"status": before.Status},
				After:    map[string]string{"status": next.Status, "note": note},
			})
		})
		switch {
		case errors.Is(err, store.ErrBadTransition):
			s.renderError(w, r, http.StatusConflict, "That unit has already moved",
				"Reload the vehicle and try again.")
			return
		case errors.Is(err, store.ErrNotFound):
			s.renderError(w, r, http.StatusNotFound, "Vehicle not found", "This unit may have been removed.")
			return
		case err != nil:
			s.internalError(w, r, "vehicle status", err)
			return
		}

		http.Redirect(w, r, "/vehicles/"+after.ID.String()+"?saved=1", http.StatusSeeOther)
	}
}

func (s *Server) renderVehicleForm(w http.ResponseWriter, r *http.Request, tenantName, userName string, status int, d views.VehicleFormData) {
	d.TenantName = tenantName
	d.UserName = userName
	d.Nav = navLinks(r.Context(), r.URL.Path)
	d.CSRFToken = csrfToken(r.Context())
	s.render(w, r, status, views.VehicleForm(d))
}

// readVehicleForm normalises the typed fields, validates them and parses the
// money and dates into the store shape. The fields come back too, so a form
// with an error keeps what the person entered.
func readVehicleForm(r *http.Request, now time.Time) (views.VehicleFields, store.VehicleParams, string) {
	f := views.VehicleFields{
		StockNo:    strings.TrimSpace(r.PostFormValue("stock_no")),
		Make:       strings.TrimSpace(r.PostFormValue("make")),
		Model:      strings.TrimSpace(r.PostFormValue("model")),
		Variant:    strings.TrimSpace(r.PostFormValue("variant")),
		ChassisNo:  strings.ToUpper(strings.TrimSpace(r.PostFormValue("chassis_no"))),
		EngineNo:   strings.TrimSpace(r.PostFormValue("engine_no")),
		Colour:     strings.TrimSpace(r.PostFormValue("colour")),
		YOM:        strings.TrimSpace(r.PostFormValue("yom")),
		Cost:       strings.TrimSpace(r.PostFormValue("cost")),
		Price:      strings.TrimSpace(r.PostFormValue("price")),
		ReceivedAt: strings.TrimSpace(r.PostFormValue("received_at")),
		Notes:      strings.TrimSpace(r.PostFormValue("notes")),
	}
	fail := func(message string) (views.VehicleFields, store.VehicleParams, string) {
		return f, store.VehicleParams{}, message
	}

	switch {
	case f.Make == "":
		return fail("Enter the make.")
	case f.Model == "":
		return fail("Enter the model.")
	case utf8.RuneCountInString(f.Make) > 80, utf8.RuneCountInString(f.Model) > 80:
		return fail("The make or model is too long. 80 characters each at most.")
	case utf8.RuneCountInString(f.ChassisNo) < 5:
		return fail("Enter the chassis number. It is at least 5 characters.")
	case utf8.RuneCountInString(f.ChassisNo) > 40:
		return fail("The chassis number is too long. 40 characters at most.")
	case utf8.RuneCountInString(f.StockNo) > 40:
		return fail("The stock number is too long. 40 characters at most.")
	case utf8.RuneCountInString(f.EngineNo) > 40:
		return fail("The engine number is too long. 40 characters at most.")
	case utf8.RuneCountInString(f.Colour) > 40:
		return fail("The colour is too long. 40 characters at most.")
	case utf8.RuneCountInString(f.Notes) > 2000:
		return fail("The notes are too long. 2000 characters at most.")
	case f.ReceivedAt == "":
		return fail("Enter the date the unit was received.")
	}

	p := store.VehicleParams{
		StockNo:   f.StockNo,
		Make:      f.Make,
		Model:     f.Model,
		Variant:   f.Variant,
		ChassisNo: f.ChassisNo,
		EngineNo:  f.EngineNo,
		Colour:    f.Colour,
		Notes:     f.Notes,
	}

	if f.YOM != "" {
		year, err := strconv.Atoi(f.YOM)
		if err != nil || year < 1990 || year > 2100 {
			return fail("Year of manufacture must be a year between 1990 and 2100.")
		}
		y := int32(year)
		p.YOM = &y
	}

	var err error
	if p.CostCents, err = parseMoneyOrZero(f.Cost); err != nil {
		return fail("Cost: " + err.Error() + ".")
	}
	if p.PriceCents, err = parseMoneyOrZero(f.Price); err != nil {
		return fail("Price: " + err.Error() + ".")
	}

	received, err := time.Parse("2006-01-02", f.ReceivedAt)
	if err != nil {
		return fail("The received date must look like 2026-10-30.")
	}
	p.ReceivedAt = received

	return f, p, ""
}

func parseMoneyOrZero(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return money.Parse(raw)
}

func vehicleRows(vehicles []store.Vehicle) []views.VehicleRow {
	rows := make([]views.VehicleRow, 0, len(vehicles))
	for _, v := range vehicles {
		rows = append(rows, views.VehicleRow{
			ID:        v.ID.String(),
			StockNo:   v.StockNo,
			Make:      v.Make,
			Model:     v.Model,
			ChassisNo: v.ChassisNo,
			Colour:    v.Colour,
			Status:    v.Status,
			Price:     money.Format(v.PriceCents),
			Received:  v.ReceivedAt.Format("2006-01-02"),
		})
	}
	return rows
}

func vehicleFields(v store.Vehicle) views.VehicleFields {
	yom := ""
	if v.YOM != nil {
		yom = strconv.Itoa(int(*v.YOM))
	}
	return views.VehicleFields{
		StockNo:    v.StockNo,
		Make:       v.Make,
		Model:      v.Model,
		Variant:    v.Variant,
		ChassisNo:  v.ChassisNo,
		EngineNo:   v.EngineNo,
		Colour:     v.Colour,
		YOM:        yom,
		Cost:       money.Format(v.CostCents),
		Price:      money.Format(v.PriceCents),
		ReceivedAt: v.ReceivedAt.Format("2006-01-02"),
		Notes:      v.Notes,
	}
}

func vehicleAudit(v store.Vehicle) map[string]string {
	out := map[string]string{
		"stock_no":   v.StockNo,
		"make":       v.Make,
		"model":      v.Model,
		"chassis_no": v.ChassisNo,
		"colour":     v.Colour,
		"status":     v.Status,
		"price":      money.Format(v.PriceCents),
		"cost":       money.Format(v.CostCents),
	}
	if v.YOM != nil {
		out["yom"] = strconv.Itoa(int(*v.YOM))
	}
	return out
}

func vehicleDuplicateMessage(err error) string {
	if errors.Is(err, store.ErrDuplicateChassis) {
		return "A vehicle with this chassis number is already in stock."
	}
	return "That stock number is already in use."
}

func movementRows(movements []store.StockMovement) []views.MovementRow {
	rows := make([]views.MovementRow, 0, len(movements))
	for _, m := range movements {
		vehicle := strings.TrimSpace(m.StockNo + " " + m.Model)
		if vehicle == "" {
			vehicle = m.ChassisNo
		}
		rows = append(rows, views.MovementRow{
			At:      m.CreatedAt.UTC().Format("2006-01-02 15:04"),
			Kind:    m.Kind,
			Delta:   fmt.Sprintf("%+d", m.Delta),
			Vehicle: vehicle,
			From:    orDashText(m.FromStatus),
			To:      m.ToStatus,
			Actor:   deref(m.ActorName),
			Note:    m.Note,
		})
	}
	return rows
}

func orDashText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
