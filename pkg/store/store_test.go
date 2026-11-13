package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oyt2020/gocker/pkg/bundle"
)

const testID = "abcdef012345"

func newTestStore(t *testing.T) (ContainerStore, *bundle.Layout) {
	t.Helper()
	layout := bundle.NewLayout(t.TempDir())
	if err := os.MkdirAll(layout.BundleDir(testID), 0755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	return NewFileStore(layout), layout
}

func TestFileStore_PutGet(t *testing.T) {
	s, layout := newTestStore(t)

	meta := &ContainerMetadata{
		ID:        testID,
		Name:      "my-container",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Image:     "alpine.tar.gz",
		Command:   []string{"/bin/sh"},
		State:     StateCreated,
	}
	if err := s.Put(meta); err != nil {
		t.Fatalf("put: %v", err)
	}

	loaded, err := s.Get(testID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.ID != meta.ID || loaded.Name != meta.Name || loaded.State != StateCreated {
		t.Fatalf("loaded metadata mismatch: %+v", loaded)
	}

	// 원자적 쓰기 후 임시 파일이 남지 않아야 한다
	entries, err := os.ReadDir(layout.BundleDir(testID))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != bundle.MetadataFileName {
			t.Fatalf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestFileStore_GetNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Get("000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStore_RejectsInvalidID(t *testing.T) {
	s, _ := newTestStore(t)
	for _, id := range []string{"", "..", "../etc", "ABCDEF012345"} {
		if _, err := s.Get(id); err == nil {
			t.Fatalf("expected error for id %q", id)
		}
		if _, err := s.Lock(id); err == nil {
			t.Fatalf("expected lock error for id %q", id)
		}
	}
}

func TestFileStore_Lock(t *testing.T) {
	s, layout := newTestStore(t)

	unlock, err := s.Lock(testID)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.BundleDir(testID), bundle.LockFileName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	// 다른 고루틴의 잠금은 해제될 때까지 대기해야 한다
	acquired := make(chan struct{})
	go func() {
		u, err := s.Lock(testID)
		if err == nil {
			u()
		}
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatalf("second lock acquired while first is held")
	case <-time.After(100 * time.Millisecond):
	}

	unlock()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatalf("second lock not acquired after unlock")
	}
}

func TestFileStore_LockMissingBundle(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Lock("000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTransitions(t *testing.T) {
	cases := []struct {
		from, to ContainerState
		ok       bool
	}{
		{StateCreated, StateRunning, true},
		{StateCreated, StateStopped, true},
		{StateRunning, StateStopped, true},
		{StateRunning, StateRunning, true},
		{StateStopped, StateStopped, true},
		{StateStopped, StateRunning, false},
		{StateRunning, StateCreated, false},
		{StateStopped, StateCreated, false},
	}
	for _, c := range cases {
		m := &ContainerMetadata{ID: testID, State: c.from, Pid: 42}
		err := m.TransitionTo(c.to)
		if (err == nil) != c.ok {
			t.Fatalf("%s -> %s: expected ok=%v, got err=%v", c.from, c.to, c.ok, err)
		}
		if c.ok && m.State != c.to {
			t.Fatalf("%s -> %s: state not updated", c.from, c.to)
		}
		if !c.ok && m.State != c.from {
			t.Fatalf("%s -> %s: state changed on invalid transition", c.from, c.to)
		}
	}
}

func TestTransitionClearsPidWhenNotRunning(t *testing.T) {
	m := &ContainerMetadata{ID: testID, State: StateRunning, Pid: 42}
	if err := m.TransitionTo(StateStopped); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if m.Pid != 0 {
		t.Fatalf("expected pid cleared, got %d", m.Pid)
	}
}
