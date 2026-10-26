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

	id := "testcontainer01"

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
