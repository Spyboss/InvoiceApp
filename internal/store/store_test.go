package store_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spyboss/invoiceapp/internal/db"
	"github.com/spyboss/invoiceapp/internal/db/dbtest"
	"github.com/spyboss/invoiceapp/internal/store"
)

var (
	admin   *pgxpool.Pool
	runtime *pgxpool.Pool
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()

	release, err := dbtest.Lock(ctx, dsn)
	if err != nil {
		panic(err)
	}

	admin, err = db.OpenAdmin(ctx, dsn)
	if err != nil {
		panic(err)
	}
	if _, err := db.Migrate(ctx, admin); err != nil {
		panic(err)
	}

	runtime, err = db.Open(ctx, dsn)
	if err != nil {
		panic(err)
	}

	code := m.Run()
	runtime.Close()
	admin.Close()
	release()
	os.Exit(code)
}

// freshStore empties every tenant table and returns a store on the runtime
// pool. Tests that need a database skip when TEST_DATABASE_URL is unset.
func freshStore(t *testing.T) *store.Store {
	t.Helper()
	if runtime == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if _, err := admin.Exec(context.Background(),
		`truncate table audit_log, sessions, user_permissions, users, tenants cascade`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	return store.New(runtime)
}

func newTenant(t *testing.T, st *store.Store, name, domain string) store.Tenant {
	t.Helper()
	tn, err := st.CreateTenant(context.Background(), slugOf(name), name, domain)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return tn
}

func newUser(t *testing.T, st *store.Store, tenantID uuid.UUID, email, role string) store.User {
	t.Helper()
	var u store.User
	err := st.InTenant(context.Background(), tenantID, func(q *store.Store) error {
		var err error
		u, err = q.CreateUser(context.Background(), store.CreateUserParams{
			TenantID:     tenantID,
			Email:        email,
			Name:         "Test User",
			PasswordHash: "not-a-real-hash",
			Role:         role,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func slugOf(name string) string {
	return "t-" + uuid.NewString()[:8]
}

func TestTenantIsolation(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()

	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")

	newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	newUser(t, st, beta.ID, "owner@beta.test", "OWNER")

	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		if _, err := q.UserByEmail(ctx, alpha.ID, "owner@beta.test"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("alpha can read beta's user: %v", err)
		}

		n, err := q.CountUsers(ctx, alpha.ID)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("alpha sees %d users, want 1", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("in tenant: %v", err)
	}
}

// Without a tenant setting, row level security must return nothing rather
// than everything.
func TestRLSFailsClosed(t *testing.T) {
	freshStore(t)
	ctx := context.Background()

	alpha := newTenant(t, store.New(runtime), "Alpha Motors", "alpha.test")
	newUser(t, store.New(runtime), alpha.ID, "owner@alpha.test", "OWNER")

	var users, audit int
	if err := runtime.QueryRow(ctx, `select count(*) from users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 0 {
		t.Fatalf("untenanted query saw %d users, want 0", users)
	}
	if err := runtime.QueryRow(ctx, `select count(*) from audit_log`).Scan(&audit); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if audit != 0 {
		t.Fatalf("untenanted query saw %d audit rows, want 0", audit)
	}
}

func TestPermissionsComeFromRole(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()

	tn := newTenant(t, st, "Alpha Motors", "alpha.test")
	owner := newUser(t, st, tn.ID, "owner@alpha.test", "OWNER")
	sales := newUser(t, st, tn.ID, "sales@alpha.test", "SALES")

	var ownerPerms, salesPerms map[string]bool
	err := st.InTenant(ctx, tn.ID, func(q *store.Store) error {
		var err error
		ownerPerms, err = q.PermissionsFor(ctx, tn.ID, owner.ID, owner.Role)
		if err != nil {
			return err
		}
		salesPerms, err = q.PermissionsFor(ctx, tn.ID, sales.ID, sales.Role)
		return err
	})
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}

	if !ownerPerms["audit.view"] || !ownerPerms["user.manage"] {
		t.Errorf("owner permissions = %v", ownerPerms)
	}
	if salesPerms["audit.view"] {
		t.Errorf("sales has audit.view: %v", salesPerms)
	}
	if !salesPerms["report.view"] {
		t.Errorf("sales lost report.view: %v", salesPerms)
	}
}

func TestUserPermissionOverrides(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()

	tn := newTenant(t, st, "Alpha Motors", "alpha.test")
	sales := newUser(t, st, tn.ID, "sales@alpha.test", "SALES")

	// Write the override the way the settings screen will: inside a tenant
	// transaction, so rls applies.
	err := st.InTenant(ctx, tn.ID, func(*store.Store) error {
		tx, err := runtime.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, tn.ID.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into user_permissions (tenant_id, user_id, permission, granted) values ($1, $2, 'audit.view', true)`, tn.ID, sales.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	var perms map[string]bool
	err = st.InTenant(ctx, tn.ID, func(q *store.Store) error {
		var err error
		perms, err = q.PermissionsFor(ctx, tn.ID, sales.ID, sales.Role)
		return err
	})
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}
	if !perms["audit.view"] {
		t.Errorf("override not applied: %v", perms)
	}
}

func TestAuditIsScopedToTenant(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()

	alpha := newTenant(t, st, "Alpha Motors", "alpha.test")
	beta := newTenant(t, st, "Beta Motors", "beta.test")

	actor := newUser(t, st, alpha.ID, "owner@alpha.test", "OWNER")
	err := st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		return q.InsertAudit(ctx, store.AuditEntry{
			TenantID: alpha.ID,
			ActorID:  &actor.ID,
			Action:   "test.event",
			Entity:   "test",
		})
	})
	if err != nil {
		t.Fatalf("insert audit: %v", err)
	}

	var rows []store.AuditRecord
	err = st.InTenant(ctx, beta.ID, func(q *store.Store) error {
		var err error
		rows, err = q.ListAudit(ctx, beta.ID, 10)
		return err
	})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("beta sees %d audit rows, want 0", len(rows))
	}

	err = st.InTenant(ctx, alpha.ID, func(q *store.Store) error {
		var err error
		rows, err = q.ListAudit(ctx, alpha.ID, 10)
		return err
	})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "test.event" {
		t.Fatalf("alpha audit = %+v", rows)
	}
}

func TestSessionsExpireAndRevoke(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()

	tn := newTenant(t, st, "Alpha Motors", "alpha.test")
	user := newUser(t, st, tn.ID, "owner@alpha.test", "OWNER")

	insert := func(token string, expires time.Time) {
		t.Helper()
		err := st.InTenant(ctx, tn.ID, func(q *store.Store) error {
			_, err := q.CreateSession(ctx, store.CreateSessionParams{
				TenantID:  tn.ID,
				UserID:    user.ID,
				TokenHash: hashOf(token),
				IP:        "127.0.0.1",
				UserAgent: "test",
				ExpiresAt: expires,
			})
			return err
		})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
	}

	const expiredToken = "expired-token-value"
	insert(expiredToken, time.Now().Add(-time.Minute))
	err := st.InTenant(ctx, tn.ID, func(q *store.Store) error {
		_, err := q.SignedInByToken(ctx, tn.ID, hashOf(expiredToken))
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session accepted: %v", err)
	}

	const liveToken = "live-token-value"
	insert(liveToken, time.Now().Add(time.Hour))
	err = st.InTenant(ctx, tn.ID, func(q *store.Store) error {
		si, err := q.SignedInByToken(ctx, tn.ID, hashOf(liveToken))
		if err != nil {
			return err
		}
		return q.RevokeSession(ctx, tn.ID, si.Session.ID)
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	err = st.InTenant(ctx, tn.ID, func(q *store.Store) error {
		_, err := q.SignedInByToken(ctx, tn.ID, hashOf(liveToken))
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked session accepted: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	if admin == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	applied, err := db.Migrate(context.Background(), admin)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if applied != 0 {
		t.Fatalf("second migrate applied %d migrations, want 0", applied)
	}
}

func hashOf(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func TestLastOwnerCannotBeRemoved(t *testing.T) {
	st := freshStore(t)
	a := newTenant(t, st, "G", "g.local")
	o := newUser(t, st, a.ID, "o@g.local", store.RoleOwner)

	err := st.InTenant(context.Background(), a.ID, func(q *store.Store) error {
		_, err := q.UpdateUserRole(context.Background(), a.ID, o.ID, store.RoleManager)
		return err
	})
	if err == nil {
		t.Fatalf("expected refusal removing the last owner")
	}
}

func TestListUsersSeesEveryone(t *testing.T) {
	st := freshStore(t)
	a := newTenant(t, st, "H", "h.local")
	newUser(t, st, a.ID, "a@h.local", store.RoleSales)
	newUser(t, st, a.ID, "b@h.local", store.RoleAccounts)

	var users []store.User
	err := st.InTenant(context.Background(), a.ID, func(q *store.Store) error {
		u, err := q.ListUsers(context.Background(), a.ID)
		users = u
		return err
	})
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) < 2 {
		t.Fatalf("users = %d, want at least 2", len(users))
	}
}

func TestPermissionOverrideWorks(t *testing.T) {
	st := freshStore(t)
	a := newTenant(t, st, "I", "i.local")
	u := newUser(t, st, a.ID, "u@i.local", store.RoleSales)

	err := st.InTenant(context.Background(), a.ID, func(q *store.Store) error {
		return q.SetPermissionOverride(context.Background(), a.ID, u.ID, "customer.write", boolPtr(true))
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	var overrides map[string]bool
	err = st.InTenant(context.Background(), a.ID, func(q *store.Store) error {
		var err error
		overrides, err = q.PermissionOverridesFor(context.Background(), a.ID, u.ID)
		return err
	})
	if err != nil {
		t.Fatalf("read overrides: %v", err)
	}
	if !overrides["customer.write"] {
		t.Fatalf("override not granted")
	}
}

func boolPtr(b bool) *bool { return &b }

