package image

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveArgs(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "rootfs-archive")
	if err := os.WriteFile(existing, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name       string
		override   string
		args       []string
		wantRootfs string
		wantCmd    []string
	}{
		{"no args", "", nil, DefaultRootFS, []string{"/bin/sh"}},
		{"alpine only", "", []string{"alpine"}, DefaultRootFS, []string{"/bin/sh"}},
		{"alpine:latest with cmd", "", []string{"alpine:latest", "echo", "hi"}, DefaultRootFS, []string{"echo", "hi"}},
		{"tar.gz path", "", []string{"my.tar.gz", "sh"}, "my.tar.gz", []string{"sh"}},
		{"tgz path", "", []string{"img.tgz"}, "img.tgz", []string{"/bin/sh"}},
		{"existing file", "", []string{existing, "ls"}, existing, []string{"ls"}},
		{"command only", "", []string{"echo", "hello"}, DefaultRootFS, []string{"echo", "hello"}},
		{"override takes all args as command", "custom.tar", []string{"alpine", "sh"}, "custom.tar", []string{"alpine", "sh"}},
		{"override without cmd", "custom.tar", nil, "custom.tar", []string{"/bin/sh"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rootfs, cmd := ResolveArgs(c.override, c.args)
			if rootfs != c.wantRootfs {
				t.Fatalf("rootfs: got %q, want %q", rootfs, c.wantRootfs)
			}
			if !reflect.DeepEqual(cmd, c.wantCmd) {
				t.Fatalf("command: got %v, want %v", cmd, c.wantCmd)
			}
		})
	}
}

func TestDefaultCommandIsCopy(t *testing.T) {
	cmd := DefaultCommand()
	cmd[0] = "mutated"
	if DefaultCommand()[0] != "/bin/sh" {
		t.Fatalf("DefaultCommand returned shared slice")
	}
}
