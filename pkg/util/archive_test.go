package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGzWithAlpine(t *testing.T) {
	assetsTar := filepath.Join("..", "..", "assets", "alpine-minirootfs.tar.gz")
	if _, err := os.Stat(assetsTar); os.IsNotExist(err) {
		t.Skip("assets/alpine-minirootfs.tar.gz does not exist, skipping")
	}

	tempDir, err := os.MkdirTemp("", "gocker-extract-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if err := ExtractTarGz(assetsTar, tempDir); err != nil {
		t.Fatalf("failed to extract rootfs: %v", err)
	}

	// Verify essential files in Alpine rootfs
	busybox := filepath.Join(tempDir, "bin", "busybox")
	if _, err := os.Stat(busybox); err != nil {
		t.Errorf("expected /bin/busybox to exist in rootfs: %v", err)
	}

	etcOsRelease := filepath.Join(tempDir, "etc", "os-release")
	if _, err := os.Stat(etcOsRelease); err != nil {
		t.Errorf("expected /etc/os-release to exist in rootfs: %v", err)
	}
}
