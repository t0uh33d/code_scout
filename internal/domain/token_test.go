package domain

import (
	"strings"
	"testing"
	"time"
)

func TestMintedTokenHasThePrefixAndFullEntropy(t *testing.T) {
	plaintext, hash, err := MintPersonalToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(plaintext, PersonalTokenPrefix) {
		t.Errorf("token %q does not start with %q", plaintext, PersonalTokenPrefix)
	}
	// csp_ plus 43 characters of base64url for 32 bytes.
	if want := len(PersonalTokenPrefix) + 43; len(plaintext) != want {
		t.Errorf("token length %d, want %d", len(plaintext), want)
	}
	if hash != HashPersonalToken(plaintext) {
		t.Error("returned hash does not match HashPersonalToken of the plaintext")
	}
	if len(hash) != 64 {
		t.Errorf("hash length %d, want 64 hex chars", len(hash))
	}
	// The plaintext must never be derivable from what is stored, and the
	// stored form must never contain the secret.
	if strings.Contains(hash, plaintext) || strings.Contains(plaintext, hash) {
		t.Error("hash and plaintext overlap")
	}

	second, _, err := MintPersonalToken()
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if second == plaintext {
		t.Error("two mints produced the same token")
	}
}

func TestExpiredHonoursANilExpiry(t *testing.T) {
	now := time.Now()
	if (&PersonalAccessToken{}).Expired(now) {
		t.Error("a token with no expiry reported itself expired")
	}
	past := now.Add(-time.Minute)
	if !(&PersonalAccessToken{ExpiresAt: &past}).Expired(now) {
		t.Error("a token past its expiry reported itself live")
	}
	future := now.Add(time.Minute)
	if (&PersonalAccessToken{ExpiresAt: &future}).Expired(now) {
		t.Error("a token before its expiry reported itself expired")
	}
}

func TestTokenSuffixIsTheDisplayTail(t *testing.T) {
	if got := TokenSuffix("csp_abcdef"); got != "cdef" {
		t.Errorf("suffix = %q, want cdef", got)
	}
	if got := TokenSuffix("ab"); got != "ab" {
		t.Errorf("short input should come back whole, got %q", got)
	}
}
