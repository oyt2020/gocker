package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpecBuilder(t *testing.T) {
	builder := NewSpecBuilder()

	opts := SpecOptions{
		Args:     []string{"/bin/echo", "hello"},
		Terminal: false,
		Hostname: "test-box",
	}

	spec, err := builder.Build(opts)
	if err != nil {
		t.Fatalf("failed to build spec: %v", err)
	}

	if spec.Hostname != "test-box" {
		t.Errorf("expected hostname 'test-box', got %s", spec.Hostname)
	}
	if len(spec.Process.Args) != 2 || spec.Process.Args[0] != "/bin/echo" {
		t.Errorf("unexpected args: %v", spec.Process.Args)
	}
	if spec.Root.Path != "rootfs" {
		t.Errorf("expected root path 'rootfs', got %s", spec.Root.Path)
	}
	if len(spec.Linux.Namespaces) != 5 {
		t.Errorf("expected 5 default namespaces, got %d", len(spec.Linux.Namespaces))
	}

	tempDir, err := os.MkdirTemp("", "gocker-oci-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.json")
	if err := builder.SaveToFile(spec, configPath); err != nil {
		t.Fatalf("failed to save config.json: %v", err)
	}

	loadedSpec, err := builder.LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config.json: %v", err)
	}

	if loadedSpec.Hostname != spec.Hostname {
		t.Errorf("hostname mismatch after load: got %s, expected %s", loadedSpec.Hostname, spec.Hostname)
	}
}
