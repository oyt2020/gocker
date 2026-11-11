package bundle

import (
	"os"
	"testing"
)

func TestBundleManager(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocker-bundle-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	layout := NewLayout(tempDir)
	bm := NewBundleManager(layout)

	id := "c8a19d3f10be"

	if bm.Exists(id) {
		t.Fatalf("bundle should not exist before creation")
	}

	b, err := bm.Create(id, "")
	if err != nil {
		t.Fatalf("failed to create bundle: %v", err)
	}

	if b.ID != id {
		t.Errorf("expected ID %s, got %s", id, b.ID)
	}
	if !bm.Exists(id) {
		t.Errorf("expected bundle to exist after creation")
	}

	rootFSStat, err := os.Stat(b.RootFS)
	if err != nil || !rootFSStat.IsDir() {
		t.Errorf("expected rootfs directory to be created: %v", err)
	}

	gotBundle, err := bm.Get(id)
	if err != nil {
		t.Fatalf("failed to get existing bundle: %v", err)
	}
	if gotBundle.Path != b.Path {
		t.Errorf("bundle path mismatch: got %s, expected %s", gotBundle.Path, b.Path)
	}

	list, err := bm.List()
	if err != nil || len(list) != 1 || list[0] != id {
		t.Errorf("expected list to contain %s, got %v", id, list)
	}

	if err := bm.Delete(id); err != nil {
		t.Fatalf("failed to delete bundle: %v", err)
	}
	if bm.Exists(id) {
		t.Errorf("bundle should not exist after deletion")
	}
}

func TestBundleManager_RejectsInvalidAndTraversalIDs(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocker-bundle-sec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	layout := NewLayout(tempDir)
	bm := NewBundleManager(layout)

	validID := "a1b2c3d4e5f6"
	if _, err := bm.Create(validID, ""); err != nil {
		t.Fatalf("failed to create valid bundle: %v", err)
	}

	maliciousIDs := []string{
		"",
		".",
		"..",
		"../",
		"../containers",
		"../../",
		"testcontainer01",
		"A1B2C3D4E5F6",
	}

	for _, badID := range maliciousIDs {
		if bm.Exists(badID) {
			t.Errorf("Exists(%q) should return false", badID)
		}
		if _, err := bm.Create(badID, ""); err == nil {
			t.Errorf("Create(%q) should fail, got nil", badID)
		}
		if _, err := bm.Get(badID); err == nil {
			t.Errorf("Get(%q) should fail, got nil", badID)
		}
		if err := bm.Delete(badID); err == nil {
			t.Errorf("Delete(%q) should fail, got nil", badID)
		}
	}

	// Ensure the valid bundle and containers directory are untouched
	if !bm.Exists(validID) {
		t.Fatalf("valid bundle %q was unexpectedly removed", validID)
	}
}
