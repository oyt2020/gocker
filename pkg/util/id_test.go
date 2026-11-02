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
}
