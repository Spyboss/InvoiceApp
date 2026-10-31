package web

import (
	"net/http"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) usersPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		var users []store.User
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var innerErr error
			users, innerErr = q.ListUsers(r.Context(), t.ID)
			return innerErr
		})
		if err != nil {
			s.internalError(w, r, "users", err)
			return
		}
		s.render(w, r, http.StatusOK, views.Users(views.UsersData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			CSRFToken:  csrfToken(r.Context()),
			CanManage:  auth.Can(r.Context(), "user.manage"),
			Roles:      roleOptions(),
			Rows:       userRows(users),
		}))
	}
}

func (s *Server) userRolePage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.Redirect(w, r, "/users", http.StatusSeeOther)
			return
		}
		role := r.PostFormValue("role")
		if !store.ValidRole(role) {
			http.Redirect(w, r, "/users", http.StatusSeeOther)
			return
		}
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			// UpdateUserRole returns (User, error); the page only cares about it
			// passing the error through. Discard the user so the page can reload.
			_, innerErr := q.UpdateUserRole(r.Context(), t.ID, id, role)
			return innerErr
		})
		if err != nil {
			s.internalError(w, r, "user role", err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func (s *Server) userActivePage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.Redirect(w, r, "/users", http.StatusSeeOther)
			return
		}
		active := r.PostFormValue("active") == "1"
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			_, innerErr := q.SetUserActive(r.Context(), t.ID, id, active)
			return innerErr
		})
		if err != nil {
			s.internalError(w, r, "user active", err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func (s *Server) userPasswordPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.Redirect(w, r, "/users", http.StatusSeeOther)
			return
		}
		newPass := r.PostFormValue("new_password")
		if newPass == "" {
			http.Redirect(w, r, "/users", http.StatusSeeOther)
			return
		}
		hash, err := auth.HashPassword(newPass)
		if err != nil {
			http.Redirect(w, r, "/users", http.StatusSeeOther)
			return
		}
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			return q.SetPasswordHash(r.Context(), t.ID, id, hash)
		})
		if err != nil {
			s.internalError(w, r, "user password", err)
			return
		}
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			return q.RevokeSessionsFor(r.Context(), t.ID, id)
		})
		if err != nil {
			s.internalError(w, r, "user password revoke", err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func userRows(users []store.User) []views.UserRow {
	out := make([]views.UserRow, 0, len(users))
	for _, u := range users {
		activeStr := "no"
		if u.IsActive {
			activeStr = "yes"
		}
		last := "-"
		if u.LastLoginAt != nil && !u.LastLoginAt.IsZero() {
			last = u.LastLoginAt.UTC().Format("2006-01-02")
		}
		out = append(out, views.UserRow{
			ID:        u.ID.String(),
			Name:      u.Name,
			Email:     u.Email,
			Role:      store.RoleLabel(u.Role),
			RoleKey:   u.Role,
			Active:    u.IsActive,
			ActiveStr: activeStr,
			LastLogin: last,
		})
	}
	return out
}

func roleOptions() []views.RoleOption {
	out := make([]views.RoleOption, 0, len(store.Roles))
	for _, r := range store.Roles {
		out = append(out, views.RoleOption{Key: r, Label: store.RoleLabel(r)})
	}
	return out
}
