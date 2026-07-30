package utils

import (
	"crypto/rand"
	"math/big"
)

// GenerateRandomString returns a cryptographically random string of the given
// length. Used for project secrets — must never be predictable.
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	max := big.NewInt(int64(len(charset)))
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand only fails if the OS entropy source is broken —
			// refusing to mint a secret is safer than falling back.
			panic("crypto/rand unavailable: " + err.Error())
		}
		result[i] = charset[n.Int64()]
	}
	return string(result)
}
