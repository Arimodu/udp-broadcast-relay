package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

const (
	APIKeyPrefix = "ubr_"
	APIKeyLength = 32
)

// GenerateAPIKey creates a new API key with the "ubr_" prefix.
func GenerateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	encoded := base64.RawURLEncoding.EncodeToString(b)
	if len(encoded) > APIKeyLength {
		encoded = encoded[:APIKeyLength]
	}
	return APIKeyPrefix + encoded
}

// GenerateSessionToken creates a cryptographically random session token.
func GenerateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
