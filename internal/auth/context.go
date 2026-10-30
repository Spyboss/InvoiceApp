package auth

import (
	"context"

	"github.com/spyboss/invoiceapp/internal/store"
)

// SignedIn is the user behind a request plus the permissions they hold.
type SignedIn struct {
	Session     store.Session
	User        store.User
	Permissions map[string]bool
}

type ctxKey struct{}

// WithUser attaches the signed in user to the request context.
func WithUser(ctx context.Context, u SignedIn) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// User returns the signed in user, or false for an anonymous request.
func User(ctx context.Context) (SignedIn, bool) {
	u, ok := ctx.Value(ctxKey{}).(SignedIn)
	return u, ok
}

// Can reports whether the signed in user holds a permission. Anonymous
// requests hold nothing.
func Can(ctx context.Context, permission string) bool {
	u, ok := User(ctx)
	if !ok {
		return false
	}
	return u.Permissions[permission]
}
