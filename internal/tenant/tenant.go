// Package tenant resolves which dealership a request belongs to and carries
// that tenant through the request context.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/store"
)

// ErrUnknownHost means no tenant claims this hostname.
var ErrUnknownHost = errors.New("unknown host")

type ctxKey struct{}

// Resolver maps a hostname to a tenant, with a short cache so every static
// asset does not hit the database.
type Resolver struct {
	store   *store.Store
	devSlug string
	ttl     time.Duration
	now     func() time.Time

	mu    sync.RWMutex
	cache map[string]entry
}

type entry struct {
	tenant  store.Tenant
	expires time.Time
}

func NewResolver(s *store.Store, devSlug string) *Resolver {
	return &Resolver{
		store:   s,
		devSlug: devSlug,
		ttl:     60 * time.Second,
		now:     time.Now,
		cache:   make(map[string]entry),
	}
}

// ByHost resolves a hostname to a tenant. Localhost resolves through the
// configured dev slug so a developer does not need hosts file entries.
func (r *Resolver) ByHost(ctx context.Context, host string) (store.Tenant, error) {
	host = NormaliseHost(host)
	if host == "" {
		return store.Tenant{}, ErrUnknownHost
	}

	r.mu.RLock()
	cached, ok := r.cache[host]
	r.mu.RUnlock()
	if ok && r.now().Before(cached.expires) {
		return cached.tenant, nil
	}

	var (
		t   store.Tenant
		err error
	)
	if isLocalHost(host) && r.devSlug != "" {
		t, err = r.store.TenantBySlug(ctx, r.devSlug)
	} else {
		t, err = r.store.TenantByDomain(ctx, host)
	}
	if errors.Is(err, store.ErrNotFound) {
		return store.Tenant{}, ErrUnknownHost
	}
	if err != nil {
		return store.Tenant{}, fmt.Errorf("resolve tenant for %q: %w", host, err)
	}

	r.mu.Lock()
	r.cache[host] = entry{tenant: t, expires: r.now().Add(r.ttl)}
	r.mu.Unlock()
	return t, nil
}

// Forget drops a hostname from the cache, used after a tenant is created.
func (r *Resolver) Forget(host string) {
	host = NormaliseHost(host)
	r.mu.Lock()
	delete(r.cache, host)
	r.mu.Unlock()
}

// NormaliseHost lowercases a host and removes the port and any trailing dot.
func NormaliseHost(raw string) string {
	host := strings.TrimSpace(raw)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// WithTenant stores the tenant in the request context.
func WithTenant(ctx context.Context, t store.Tenant) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// FromContext returns the tenant, or false when the request has none.
func FromContext(ctx context.Context) (store.Tenant, bool) {
	t, ok := ctx.Value(ctxKey{}).(store.Tenant)
	return t, ok && t.ID != uuid.Nil
}
