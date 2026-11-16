package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const testID = "abcdef012345"

func newTestStore(t *testing.T) (ContainerStore, string) {
	t.Helper()
	// 저장소 디렉터리는 미리 만들지 않는다: Put / Lock이 필요할 때 만든다
	dir := filepath.Join(t.TempDir(), "state")
	return NewFileStore(dir), dir
}

func newTestMeta(id string) *ContainerMetadata {
	return &ContainerMetadata{
		ID:        id,
		Name:      "my-container",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Image:     "alpine.tar.gz",
		Command:   []string{"/bin/sh"},
		State:     StateCreated,
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("readdir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestFileStore_PutGet(t *testing.T) {
	s, dir := newTestStore(t)

	meta := newTestMeta(testID)
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
	if names := dirNames(t, dir); len(names) != 1 || names[0] != testID+".json" {
		t.Fatalf("unexpected files in store dir: %v", names)
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
		if err := s.Delete(id); err == nil {
			t.Fatalf("expected delete error for id %q", id)
		}
	}
}

func TestFileStore_Delete(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Put(newTestMeta(testID)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.Delete(testID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(testID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	// 멱등: 이미 없는 레코드 삭제는 성공
	if err := s.Delete(testID); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestFileStore_List(t *testing.T) {
	s, dir := newTestStore(t)

	// 저장소 디렉터리가 아직 없어도 빈 목록
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty list: %v, %v", list, err)
	}

	ids := []string{"bbbbbbbbbbbb", "aaaaaaaaaaaa"}
	for _, id := range ids {
		if err := s.Put(newTestMeta(id)); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	// 잠금 파일 / 임시 파일 / 레코드가 아닌 파일은 목록에 나오지 않아야 한다
	unlock, err := s.Lock("cccccccccccc")
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer unlock()
	for _, name := range []string{".aaaaaaaaaaaa.json.tmp-1", "notes.json", "README"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != "aaaaaaaaaaaa" || list[1].ID != "bbbbbbbbbbbb" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestFileStore_Lock(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Put(newTestMeta(testID)); err != nil {
		t.Fatalf("put: %v", err)
	}

	unlock, err := s.Lock(testID)
	if err != nil {
		t.Fatalf("lock: %v", err)
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

// 레코드가 없는 ID도 잠글 수 있고(생성 보호), 해제 시 잠금 파일이 남지 않아야 한다
// 저장소 전역 잠금: 한 번에 하나만 잡을 수 있고, 풀고 나면 잠금 파일이 남지 않는다
func TestFileStore_LockNames(t *testing.T) {
	s, dir := newTestStore(t)

	unlock, err := s.LockNames()
	if err != nil {
		t.Fatalf("lock names: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		u, err := s.LockNames()
		if err == nil {
			u()
		}
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatalf("second names lock acquired while first is held")
	case <-time.After(100 * time.Millisecond):
	}

	unlock()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatalf("second names lock not acquired after unlock")
	}

	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("names lock must leave nothing behind, got %v", names)
	}

	// 잠금 파일은 레코드 목록에 섞이지 않는다
	unlock, err = s.LockNames()
	if err != nil {
		t.Fatalf("lock names again: %v", err)
	}
	defer unlock()
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("list while names lock is held = %+v, %v; want empty", list, err)
	}
}

func TestFileStore_LockWithoutRecordLeavesNothing(t *testing.T) {
	s, dir := newTestStore(t)

	unlock, err := s.Lock(testID)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	unlock()
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("lock file leaked for non-existent container: %v", names)
	}

	// 레코드가 있으면 잠금 파일은 유지된다
	if err := s.Put(newTestMeta(testID)); err != nil {
		t.Fatalf("put: %v", err)
	}
	unlock, err = s.Lock(testID)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	unlock()
	if names := dirNames(t, dir); len(names) != 2 {
		t.Fatalf("expected record + lock file, got %v", names)
	}
}

// 삭제하는 쪽이 잠금 파일을 지워도, 대기하던 쪽과 새로 들어온 쪽이 동시에 잠금을 갖는 일이 없어야 한다
func TestFileStore_LockSurvivesLockFileRemoval(t *testing.T) {
	s, _ := newTestStore(t)

	const workers = 8
	const rounds = 50
	var inside atomic.Int32 // 현재 잠금을 쥐고 있는 고루틴 수
	errCh := make(chan error, workers)

	for w := 0; w < workers; w++ {
		go func() {
			for i := 0; i < rounds; i++ {
				unlock, err := s.Lock(testID)
				if err != nil {
					errCh <- err
					return
				}
				if inside.Add(1) != 1 {
					unlock()
					errCh <- errors.New("two holders inside the lock at once")
					return
				}
				inside.Add(-1)
				unlock() // 레코드가 없으므로 매번 잠금 파일을 지운다
			}
			errCh <- nil
		}()
	}
	for w := 0; w < workers; w++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
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
