package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetadataSaveLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocker-meta-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	metaPath := filepath.Join(tempDir, "metadata.json")

	meta := &ContainerMetadata{
		ID:         "testmeta01",
		Name:       "my-container",
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		Image:      "alpine.tar.gz",
		Command:    []string{"/bin/sh"},
		BundlePath: "/var/lib/gocker/containers/testmeta01",
		State:      StateCreated,
		Pid:        12345,
	}

	if err := SaveMetadata(meta, metaPath); err != nil {
		t.Fatalf("failed to save metadata: %v", err)
	}

	loaded, err := LoadMetadata(metaPath)
	if err != nil {
		t.Fatalf("failed to load metadata: %v", err)
	}

	if loaded.ID != meta.ID {
		t.Errorf("ID mismatch: got %s, expected %s", loaded.ID, meta.ID)
	}
	if loaded.State != StateCreated {
		t.Errorf("State mismatch: got %s, expected %s", loaded.State, StateCreated)
	}
	if loaded.Pid != 12345 {
		t.Errorf("PID mismatch: got %d, expected %d", loaded.Pid, 12345)
	}
}
