// Package auth owns password hashing, session tokens, csrf tokens and the
// signed in user carried through a request.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// ErrInvalidHash means a stored hash is not in the format this package wrote.
var ErrInvalidHash = errors.New("invalid password hash")

// OWASP argon2id recommendation for interactive logins, tuned up: 64 MiB,
// three passes, two lanes, roughly 100ms on a laptop and slower on a phone
// class cpu, which is the point.
const (
	argonMemoryKiB = 64 * 1024
	argonPasses    = 3
	argonLanes     = 2
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// HashPassword returns a self describing argon2id hash. The parameters travel
// with the hash so they can be raised later without breaking old accounts.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonPasses, argonMemoryKiB, argonLanes, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonPasses, argonLanes,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword compares a password with a stored hash in constant time.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}

	var memory, passes uint32
	var lanes uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &lanes); err != nil {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	got := argon2.IDKey([]byte(password), salt, passes, memory, lanes, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash gives unknown accounts the same work as real ones, so response
// time does not reveal which emails exist.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("timing-equaliser-not-a-real-password")
	if err != nil {
		return ""
	}
	return h
})

// BurnPasswordTime runs the same argon2 work as a real verification.
func BurnPasswordTime(password string) {
	if h := dummyHash(); h != "" {
		_, _ = VerifyPassword(h, password)
	}
}

// ValidatePassword is the only password policy. Length is the property that
// matters; composition rules push people to predictable substitutions.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(password) > 200 {
		return errors.New("password must be at most 200 characters")
	}
	return nil
}
