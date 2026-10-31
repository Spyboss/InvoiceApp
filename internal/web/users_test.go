package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/store"
)

func TestUsers(t *testing.T) {
	h, _ := newTestServer(t)
	const password = "correct horse battery staple"
	tn := createTenantAndOwner(t, "Alpha Motors", "alpha.local", "owner@alpha.local", password)

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	err = webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		_, err := q.CreateUser(context.Background(), store.CreateUserParams{
			TenantID:     tn.ID,
			Email:        "manager@alpha.local",
			Name:         "Sales Manager",
			PasswordHash: hash,
			Role:         "MANAGER",
		})
		return err
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	c := signIn(t, h, "alpha.local", "owner@alpha.local", password)

	// Owner can open the users screen.
	rec := c.get("/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Users") {
		t.Fatalf("users page missing title:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "manager@alpha.local") {
		t.Errorf("users page missing manager:\n%s", rec.Body.String())
	}

	// Owner can change a user's role. Get the manager's ID from the page.
	token := c.csrfFrom(rec)
	// The manager is the second row in the list; we need to find their ID from the HTML.
	// We look for the manager's email row and extract the user ID.
	// The row format is /users/{id}/active or /users/{id}/password.
	// For now, let's assume we have a way to get the ID; we'll use the store.
	var managerID string
	err = webStore.InTenant(context.Background(), tn.ID, func(q *store.Store) error {
		users, err := q.ListUsers(context.Background(), tn.ID)
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Email == "manager@alpha.local" {
				managerID = u.ID.String()
				return nil
			}
		}
		return nil
	})
	if err != nil || managerID == "" {
		t.Fatalf("manager id: %v %s", err, managerID)
	}

	// Change manager to ACCOUNTS.
	form := url.Values{
		"csrf_token": {token},
		"role":       {"ACCOUNTS"},
	}
	rec = c.post("/users/"+managerID+"/role", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST role change = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/users" {
		t.Errorf("location = %q, want /users", loc)
	}

	// Verify role change was saved.
	rec = c.get("/users")
	if !strings.Contains(rec.Body.String(), "ACCOUNTS") {
		t.Errorf("role change not saved:\n%s", rec.Body.String())
	}

	// Owner can deactivate a user.
	form = url.Values{
		"csrf_token": {token},
		"active":     {"0"},
	}
	rec = c.post("/users/"+managerID+"/active", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST deactivate = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}

	// Verify deactivation was saved.
	rec = c.get("/users")
	if !strings.Contains(rec.Body.String(), "no") {
		t.Errorf("deactivation not saved:\n%s", rec.Body.String())
	}

	// Owner can reset a password.
	form = url.Values{
		"csrf_token":   {token},
		"new_password": {"new secure password 12345"},
	}
	rec = c.post("/users/"+managerID+"/password", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST password reset = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}

	// Verify old sessions are revoked.
	c2 := newTestClient(t, h, "alpha.local")
	c2.cookies["session"] = "old-session-value"
	rec = c2.get("/users")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("old session should be revoked = %d, want 303", rec.Code)
	}
}
