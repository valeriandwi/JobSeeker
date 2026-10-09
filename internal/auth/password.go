// Package auth implements password authentication and server-side sessions.
//
// Passwords are hashed with Argon2id (PHC-encoded strings). Sessions are random
// 256-bit tokens; only their SHA-256 digest is stored, so a database leak does
// not expose usable session tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Chosen for interactive login on a modern laptop
// (~64 MiB, 3 passes, 2 lanes) per OWASP guidance.
const (
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLen     = 16
	argonKeyLen      = 32
)

// ErrInvalidHash is returned when a stored hash cannot be parsed.
var ErrInvalidHash = errors.New("invalid password hash format")

// HashPassword returns a PHC-encoded Argon2id hash of the password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword compares a plaintext password against a PHC-encoded hash in
// constant time. ok is false for mismatches; err is non-nil only when the
// stored hash is malformed.
func VerifyPassword(password, encoded string) (ok bool, err error) {
	params, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	computed := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(key)))
	return subtle.ConstantTimeCompare(computed, key) == 1, nil
}

// NeedsRehash reports whether a stored hash uses weaker parameters than the
// current policy and should be upgraded on next successful login.
func NeedsRehash(encoded string) bool {
	p, _, _, err := decodeHash(encoded)
	if err != nil {
		return true
	}
	return p.memory != argonMemory || p.iterations != argonIterations || p.parallelism != argonParallelism
}

type argonParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	var p argonParams
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, fmt.Errorf("%w: unsupported version", ErrInvalidHash)
	}
	var mem, iter uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return p, nil, nil, fmt.Errorf("%w: bad parameters", ErrInvalidHash)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, fmt.Errorf("%w: bad salt encoding", ErrInvalidHash)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, fmt.Errorf("%w: bad key encoding", ErrInvalidHash)
	}
	p = argonParams{memory: mem, iterations: iter, parallelism: par}
	return p, salt, key, nil
}

// NewSessionToken returns a new opaque session token and its storage digest.
func NewSessionToken() (raw string, digest []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("generating session token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, TokenDigest(raw), nil
}

// TokenDigest hashes a session token for storage.
func TokenDigest(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// Email validation and normalization ----------------------------------------

const (
	minPasswordLen = 10
	maxPasswordLen = 200
	maxEmailLen    = 254
)

// NormalizeEmail lowercases and trims an address (storage keeps the user's
// original spelling only if they registered with mixed case; lookups are always
// case-insensitive).
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail performs structural validation only. Deliverability is not
// verified and must not be assumed.
func ValidateEmail(email string) error {
	e := NormalizeEmail(email)
	if e == "" {
		return errors.New("email is required")
	}
	if len(e) > maxEmailLen {
		return errors.New("email is too long")
	}
	at := strings.LastIndex(e, "@")
	if at <= 0 || at == len(e)-1 {
		return errors.New("email must contain a local part and a domain")
	}
	local, domain := e[:at], e[at+1:]
	if len(local) > 64 {
		return errors.New("email local part is too long")
	}
	if strings.ContainsAny(e, " \t\r\n") {
		return errors.New("email must not contain whitespace")
	}
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return errors.New("email domain looks invalid")
	}
	if strings.Contains(domain, "..") {
		return errors.New("email domain looks invalid")
	}
	return nil
}

// ValidatePassword enforces the password policy. The upper bound exists to
// bound Argon2 work, not as a security requirement.
func ValidatePassword(password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("password must be at least %d characters", minPasswordLen)
	}
	if len(password) > maxPasswordLen {
		return fmt.Errorf("password must be at most %d characters", maxPasswordLen)
	}
	return nil
}
