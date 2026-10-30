package auth

import (
	"sync"
	"time"
)

// Limiter is a per key sliding window counter for failed logins. It keeps
// memory bounded by pruning expired keys when the map grows.
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   make(map[string][]time.Time),
	}
}

// Allow records an attempt and reports whether it fits under the limit.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	now := l.now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	recent := l.hits[key][:0]
	for _, at := range l.hits[key] {
		if at.After(cutoff) {
			recent = append(recent, at)
		}
	}

	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}

	l.hits[key] = append(recent, now)
	if len(l.hits) > 10_000 {
		l.prune(cutoff)
	}
	return true
}

// Reset clears a key after a successful login.
func (l *Limiter) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

func (l *Limiter) prune(cutoff time.Time) {
	for key, times := range l.hits {
		if len(times) == 0 || times[len(times)-1].Before(cutoff) {
			delete(l.hits, key)
		}
	}
}
