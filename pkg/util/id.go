package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateID creates a random 12-character hexadecimal container identifier.
func GenerateID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
