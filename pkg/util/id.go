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

// ValidateID checks that id is strictly a 12-character lowercase hexadecimal string ([0-9a-f]{12}).
func ValidateID(id string) error {
	if !IsValidID(id) {
		return fmt.Errorf("invalid container id %q: must be 12 lowercase hex characters", id)
	}
	return nil
}

// IsValidID reports whether id matches ^[0-9a-f]{12}$.
func IsValidID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
