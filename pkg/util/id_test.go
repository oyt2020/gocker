package util

import (
	"encoding/hex"
	"testing"
)

func TestGenerateID(t *testing.T) {
	id1, err := GenerateID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id1) != 12 {
		t.Fatalf("expected 12 characters, got %d (%s)", len(id1), id1)
	}

	decoded, err := hex.DecodeString(id1)
	if err != nil || len(decoded) != 6 {
		t.Fatalf("expected valid 6-byte hex string, got error: %v", err)
	}

	id2, err := GenerateID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("expected unique IDs, got identical: %s", id1)
	}
	if err := ValidateID(id1); err != nil {
		t.Fatalf("expected generated ID %q to be valid, got error: %v", id1, err)
	}
}

func TestValidateID(t *testing.T) {
	validIDs := []string{
		"0123456789ab",
		"c8a19d3f10be",
		"ffffffffffff",
		"000000000000",
	}
	for _, id := range validIDs {
		if !IsValidID(id) {
			t.Errorf("expected %q to be valid", id)
		}
		if err := ValidateID(id); err != nil {
			t.Errorf("unexpected error for valid id %q: %v", id, err)
		}
	}

	invalidIDs := []string{
		"",
		".",
		"..",
		"../etc/passwd",
		"../../..",
		"c8a19d3f10b",    // 11 chars
		"c8a19d3f10be0",  // 13 chars
		"C8A19D3F10BE",   // uppercase
		"c8a19d3f10bg",   // non-hex 'g'
		"c8a19d3f/0be",   // slash
		"testcontainer1", // non-hex & wrong length
	}
	for _, id := range invalidIDs {
		if IsValidID(id) {
			t.Errorf("expected %q to be invalid", id)
		}
		if err := ValidateID(id); err == nil {
			t.Errorf("expected error for invalid id %q, got nil", id)
		}
	}
}
