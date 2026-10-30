package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash format = %q", hash)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("correct password rejected")
	}

	ok, err = VerifyPassword(hash, "correct horse battery stapl")
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if ok {
		t.Fatal("wrong password accepted")
	}
}

func TestVerifyRejectsForeignHash(t *testing.T) {
	for _, encoded := range []string{
		"",
		"plaintext",
		"$2y$10$abcdefghijklmnopqrstuv",
		"$argon2id$v=19$m=65536,t=3,p=2$notbase64$notbase64",
	} {
		if _, err := VerifyPassword(encoded, "whatever"); err == nil {
			t.Errorf("VerifyPassword(%q): want error", encoded)
		}
	}
}

func TestHashIsSalted(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Error("short password accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", 12)); err != nil {
		t.Errorf("12 character password rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 201)); err == nil {
		t.Error("over long password accepted")
	}
}

func TestCSRFValid(t *testing.T) {
	token, err := NewCSRFToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	if !CSRFValid(token, token) {
		t.Error("matching tokens rejected")
	}
	if CSRFValid(token, token+"x") {
		t.Error("different lengths accepted")
	}
	if CSRFValid(token, strings.Repeat("a", len(token))) {
		t.Error("different value accepted")
	}
	if CSRFValid("", token) {
		t.Error("empty cookie accepted")
	}
	if CSRFValid(token, "") {
		t.Error("empty form value accepted")
	}
}

func TestTokensAreUniqueAndHashed(t *testing.T) {
	a, hashA, err := NewToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	b, _, err := NewToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if a == b {
		t.Fatal("two tokens are identical")
	}
	if string(hashA) == a {
		t.Fatal("stored hash equals the plaintext token")
	}
	if string(HashToken(a)) != string(hashA) {
		t.Fatal("HashToken disagrees with NewToken")
	}
}

func TestLimiterWindow(t *testing.T) {
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	l := NewLimiter(2, time.Minute)
	l.now = func() time.Time { return now }

	if !l.Allow("ip") {
		t.Fatal("first attempt blocked")
	}
	if !l.Allow("ip") {
		t.Fatal("second attempt blocked")
	}
	if l.Allow("ip") {
		t.Fatal("third attempt allowed")
	}
	if !l.Allow("other") {
		t.Fatal("separate key blocked")
	}

	now = now.Add(61 * time.Second)
	if !l.Allow("ip") {
		t.Fatal("attempt blocked after the window passed")
	}

	l.Reset("ip")
	if !l.Allow("ip") || !l.Allow("ip") {
		t.Fatal("reset did not clear the key")
	}
}
