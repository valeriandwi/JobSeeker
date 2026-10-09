package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected PHC prefix: %q", hash)
	}
	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("expected the correct password to verify")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password must differ (unique salt)")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("right-password")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword returned error for a valid hash: %v", err)
	}
	if ok {
		t.Fatal("expected a wrong password to fail verification")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for name, encoded := range map[string]string{
		"empty":         "",
		"not-phc":       "plaintext",
		"wrong-algo":    "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$a2V5",
		"bad-version":   "$argon2id$v=13$m=64,t=1,p=1$c2FsdA$a2V5",
		"bad-params":    "$argon2id$v=19$m=x,t=1,p=1$c2FsdA$a2V5",
		"bad-b64-salt":  "$argon2id$v=19$m=64,t=1,p=1$!!!$a2V5",
		"too-few-parts": "$argon2id$v=19$m=64,t=1,p=1",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyPassword("whatever", encoded); err == nil {
				t.Fatalf("expected an error for %q", encoded)
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if NeedsRehash(current) {
		t.Fatal("a hash produced with current parameters must not need rehashing")
	}
	// Weaker parameters than the current policy.
	weak := "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5"
	if !NeedsRehash(weak) {
		t.Fatal("a hash with weaker parameters must need rehashing")
	}
	if !NeedsRehash("garbage") {
		t.Fatal("an unparsable hash must need rehashing")
	}
}

func TestSessionTokenDigest(t *testing.T) {
	raw, digest, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || len(digest) != 32 {
		t.Fatalf("unexpected token/digest: %q %d bytes", raw, len(digest))
	}
	if got := TokenDigest(raw); string(got) != string(digest) {
		t.Fatal("TokenDigest must be deterministic for the same token")
	}
	other, _, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if other == raw {
		t.Fatal("two session tokens must differ")
	}
	if string(TokenDigest(other)) == string(digest) {
		t.Fatal("different tokens must produce different digests")
	}
	// The raw token must not be recoverable from the digest.
	if strings.Contains(raw, string(digest)) {
		t.Fatal("digest must not contain the raw token")
	}
}

func TestValidateEmail(t *testing.T) {
	valid := []string{"a@b.co", "user.name+tag@example.com", "UPPER@EXAMPLE.COM"}
	for _, e := range valid {
		if err := ValidateEmail(e); err != nil {
			t.Errorf("expected %q to be valid: %v", e, err)
		}
	}
	invalid := []string{"", "no-at-sign", "@example.com", "user@", "user@localhost", "a b@example.com", "user@exa..mple.com", "user@.com", "user@com."}
	for _, e := range invalid {
		if err := ValidateEmail(e); err == nil {
			t.Errorf("expected %q to be invalid", e)
		}
	}
	if got := NormalizeEmail("  Mixed@Case.COM "); got != "mixed@case.com" {
		t.Errorf("NormalizeEmail = %q", got)
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", minPasswordLen)); err != nil {
		t.Errorf("minimum length must be accepted: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", minPasswordLen-1)); err == nil {
		t.Error("too-short password must be rejected")
	}
	if err := ValidatePassword(strings.Repeat("a", maxPasswordLen+1)); err == nil {
		t.Error("over-long password must be rejected")
	}
}
