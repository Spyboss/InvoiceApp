package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// NewToken returns a session token for the cookie and the hash that is stored
// in the database. The plaintext never touches the database.
func NewToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken hashes a cookie value the same way NewToken does.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewCSRFToken returns a random token for the double submit cookie.
func NewCSRFToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("csrf token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// CSRFValid compares the cookie and the submitted form value in constant time.
func CSRFValid(cookieValue, formValue string) bool {
	if len(cookieValue) < 32 || len(formValue) < 32 {
		return false
	}
	if len(cookieValue) != len(formValue) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookieValue), []byte(formValue)) == 1
}
