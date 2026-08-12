package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// PersonalTokenPrefix marks a personal access token on sight. Secret scanners
// key on prefixes, so a token that leaks into a repo or a chat log is
// something a grep can find, and the middleware can refuse a non-token bearer
// before touching the database.
const PersonalTokenPrefix = "csp_"

// personalTokenBytes is the secret's entropy. 32 bytes is 256 bits, which is
// what makes storing a plain SHA-256 sound: nobody preimage-searches 256 bits
// of crypto/rand output, so the hash needs no salt and no bcrypt, and lookup
// stays one indexed read.
const personalTokenBytes = 32

// PersonalAccessToken authenticates the MCP endpoint. It belongs to a user and
// grants exactly what that user can already see; it is not a project secret.
//
// TokenHash is all that is ever stored. The plaintext exists once, in the
// return value of MintPersonalToken, and is shown to its owner exactly once.
type PersonalAccessToken struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Name   string
	// TokenHash is hex SHA-256 of the plaintext.
	TokenHash string
	// Suffix is the last four characters of the plaintext, kept so the owner
	// can tell csp_…ab12 from csp_…9f3e in a list. Four characters of a
	// 43-character secret identify without helping anyone guess.
	Suffix string
	// LastUsedAt answers "is anything still using this?" before a revoke. It
	// is written at most once per few minutes, so it is an approximation.
	LastUsedAt *time.Time
	// ExpiresAt nil means the token never expires.
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// Expired reports whether the token is past its expiry. A nil expiry never
// expires.
func (t *PersonalAccessToken) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && now.After(*t.ExpiresAt)
}

// MintPersonalToken generates a new token, returning the plaintext and the
// hash to store. The caller shows the plaintext once and keeps only the hash.
func MintPersonalToken() (plaintext, hash string, err error) {
	raw := make([]byte, personalTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = PersonalTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return plaintext, HashPersonalToken(plaintext), nil
}

// HashPersonalToken maps a plaintext token to the stored form. Deterministic
// on purpose: the middleware recomputes it on every request and looks the
// result up on a unique index.
func HashPersonalToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// TokenSuffix is the display tail of a plaintext token, for the tokens list.
func TokenSuffix(plaintext string) string {
	if len(plaintext) < 4 {
		return plaintext
	}
	return plaintext[len(plaintext)-4:]
}
