package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/oyt2020/gocker/pkg/bundle"
	"github.com/oyt2020/gocker/pkg/runc"
	"github.com/oyt2020/gocker/pkg/store"
)

// fakeConsole : 실제 소켓 없이 PTY master 역할의 파이프를 넘겨주는 runc.Console
type fakeConsole struct {
	mu      sync.Mutex
	master  *os.File // 아직 넘겨주지 않은 master (파이프의 읽기 쪽)
	slave   *os.File // 컨테이너 쪽 (파이프의 쓰기 쪽)
	closed  int
	waitErr error
}

func (c *fakeConsole) Path() string { return "/fake/console.sock" }

func (c *fakeConsole) WaitMaster(ctx context.Context) (*os.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.waitErr != nil {
		return nil, c.waitErr
	}
	if c.master == nil {
		return nil, errors.New("no master")
	}
	m := c.master
	c.master = nil
	return m, nil
}

func (c *fakeConsole) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	if c.master != nil {
		_ = c.master.Close()
		c.master = nil
	}
	return nil
}

// fakeRunc : runc 바이너리 없이 상태만 흉내 내는 runc.RuncClient
type fakeRunc struct {
	mu        sync.Mutex
	status    map[string]string
	createErr error
	startErr  error
	waitErr   error  // Console.WaitMaster가 반환할 에러
	output    string // start 시 컨테이너가 터미널에 쓰는 출력
	console   *fakeConsole
	deleted   []string
}

func newFakeRunc() *fakeRunc {
	return &fakeRunc{status: map[string]string{}}
}

func (f *fakeRunc) NewConsole() (runc.Console, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.console = &fakeConsole{waitErr: f.waitErr}
	return f.console, nil
}

func (f *fakeRunc) Create(ctx context.Context, id, bundleDir, pidFile string, ioOpts *runc.IOOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	if ioOpts != nil && ioOpts.ConsoleSocket != "" {
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		f.console.mu.Lock()
		f.console.master, f.console.slave = r, w
		f.console.mu.Unlock()
	}
	f.status[id] = runc.StatusCreated
	return nil
}

func (f *fakeRunc) Start(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	// 컨테이너가 출력을 쓰고 곧바로 종료한 상황: slave가 닫혀 master 쪽 읽기가 끝난다
	if f.console != nil && f.console.slave != nil {
		_, _ = f.console.slave.Write([]byte(f.output))
		_ = f.console.slave.Close()
	}
	f.status[id] = runc.StatusStopped
	return nil
}

func (f *fakeRunc) Kill(ctx context.Context, id, signal string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = runc.StatusStopped
	return nil
}

func (f *fakeRunc) Delete(ctx context.Context, id string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.status[id]; !ok {
		return fmt.Errorf("container %q does not exist", id)
	}
	delete(f.status, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeRunc) State(ctx context.Context, id string) (*runc.RuncState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.status[id]
	if !ok {
		return nil, fmt.Errorf("container %q does not exist", id)
	}
	return &runc.RuncState{ID: id, Status: s}, nil
}

func (f *fakeRunc) Run(ctx context.Context, id, bundleDir, pidFile string, ioOpts *runc.IOOption) error {
	return nil
}

type testEnv struct {
	rt     ContainerRuntime
	runc   *fakeRunc
	bundle bundle.BundleManager
	store  store.ContainerStore
	layout *bundle.Layout
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	layout := bundle.NewLayout(t.TempDir())
	bm := bundle.NewBundleManager(layout)
	st := store.NewFileStore(layout.StateDir())
	fr := newFakeRunc()
	return &testEnv{
		rt:     NewContainerRuntime(bm, nil, fr, st),
		runc:   fr,
		bundle: bm,
		store:  st,
		layout: layout,
	}
}

// assertNoTrace : 컨테이너의 레코드, 잠금 파일, 번들이 하나도 남지 않았는지 확인
func (e *testEnv) assertNoTrace(t *testing.T, id string) {
	t.Helper()
	if _, err := e.store.Get(id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("record for %s should be gone, got err=%v", id, err)
	}
	if e.bundle.Exists(id) {
		t.Fatalf("bundle for %s should be gone", id)
	}
	entries, err := os.ReadDir(e.layout.StateDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("state dir should be empty, got %d entries (first: %s)", len(entries), entries[0].Name())
	}
}

func TestCreate_RecordIsSourceOfTruth(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	meta, err := e.rt.Create(ctx, CreateOptions{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if meta.ID == "" || meta.State != StateCreated {
		t.Fatalf("unexpected metadata: %+v", meta)
	}

	stored, err := e.store.Get(meta.ID)
	if err != nil {
		t.Fatalf("record not stored: %v", err)
	}
	if stored.BundlePath != e.layout.BundleDir(meta.ID) || !e.bundle.Exists(meta.ID) {
		t.Fatalf("bundle not created at recorded path %q", stored.BundlePath)
	}

	// 메타데이터는 번들 디렉터리 밖(state 디렉터리)에 있어야 한다
	entries, _ := os.ReadDir(e.layout.BundleDir(meta.ID))
	for _, entry := range entries {
		if entry.Name() == "metadata.json" || entry.Name() == ".lock" {
			t.Fatalf("metadata must not live in the bundle dir: %s", entry.Name())
		}
	}

	list, err := e.rt.List(ctx, true)
	if err != nil || len(list) != 1 || list[0].ID != meta.ID {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if running, _ := e.rt.List(ctx, false); len(running) != 0 {
		t.Fatalf("created container must not be listed as running: %+v", running)
	}
}

func TestCreate_DuplicateID(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	const id = "abcdef012345"

	if _, err := e.rt.Create(ctx, CreateOptions{ID: id}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.rt.Create(ctx, CreateOptions{ID: id}); err == nil {
		t.Fatalf("expected error for duplicate id")
	}
	// 실패한 두 번째 생성이 기존 컨테이너를 롤백으로 지워서는 안 된다
	if _, err := e.store.Get(id); err != nil || !e.bundle.Exists(id) {
		t.Fatalf("existing container damaged by failed duplicate create: %v", err)
	}
}

func TestCreate_RollbackLeavesNoTrace(t *testing.T) {
	e := newTestEnv(t)
	e.runc.createErr = errors.New("boom")
	const id = "abcdef012345"

	if _, err := e.rt.Create(context.Background(), CreateOptions{ID: id, Terminal: true}); err == nil {
		t.Fatalf("expected create error")
	}
	e.assertNoTrace(t, id)
	if e.runc.console.closed == 0 {
		t.Fatalf("console must be closed on failure")
	}
}

func TestCreate_MasterReceiveFailureRollsBackRuncContainer(t *testing.T) {
	e := newTestEnv(t)
	e.runc.waitErr = errors.New("timeout waiting for pty master")
	const id = "abcdef012345"

	if _, err := e.rt.Create(context.Background(), CreateOptions{ID: id, Terminal: true}); err == nil {
		t.Fatalf("expected create error")
	}
	e.assertNoTrace(t, id)
	if len(e.runc.deleted) != 1 || e.runc.deleted[0] != id {
		t.Fatalf("runc container must be deleted on rollback, deleted=%v", e.runc.deleted)
	}
	// 아무도 가져가지 않은 master는 Console.Close가 회수한다
	if _, err := e.runc.console.slave.Write([]byte("x")); err == nil {
		t.Fatalf("untaken pty master leaked")
	}
}

// create -t 는 데몬이 없으므로 master를 유지하지 않는다 (이후 start는 실패하는 것이 의도된 동작)
func TestCreate_TerminalDoesNotKeepMaster(t *testing.T) {
	e := newTestEnv(t)

	meta, err := e.rt.Create(context.Background(), CreateOptions{Terminal: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.runc.console.closed == 0 {
		t.Fatalf("console must be closed after create")
	}
	if _, err := e.runc.console.slave.Write([]byte("x")); err == nil {
		t.Fatalf("pty master must be closed when Create returns")
	}
	if _, err := e.store.Get(meta.ID); err != nil {
		t.Fatalf("record missing: %v", err)
	}
}

func TestRun_TerminalCopiesOutputAndRemoves(t *testing.T) {
	e := newTestEnv(t)
	e.runc.output = "hello from pty\r\n"

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := e.rt.Run(ctx, RunOptions{
		CreateOptions: CreateOptions{Terminal: true, Stdout: &out},
		Remove:        true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ID == "" {
		t.Fatalf("run must report the container id")
	}
	// Run이 반환된 시점에는 출력 고루틴이 끝나 있으므로 버퍼를 바로 읽어도 된다
	if out.String() != e.runc.output {
		t.Fatalf("output = %q, want %q", out.String(), e.runc.output)
	}
	e.assertNoTrace(t, res.ID)
}

func TestRun_TerminalStartFailureCleansUp(t *testing.T) {
	e := newTestEnv(t)
	e.runc.startErr = errors.New("start failed")

	done := make(chan struct{})
	var res RunResult
	var err error
	go func() {
		// 컨테이너 쪽(slave)이 열린 채로 남아 있어도 출력 고루틴을 멈추고 반환해야 한다
		res, err = e.rt.Run(context.Background(), RunOptions{CreateOptions: CreateOptions{Terminal: true}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Run blocked after start failure")
	}
	defer e.runc.console.slave.Close()

	if err == nil {
		t.Fatalf("expected start error")
	}
	e.assertNoTrace(t, res.ID)
	if len(e.runc.deleted) != 1 {
		t.Fatalf("runc container must be deleted, deleted=%v", e.runc.deleted)
	}
}

func TestStartStopTransitions(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	meta, err := e.rt.Create(ctx, CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := e.rt.Start(ctx, meta.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if stored, _ := e.store.Get(meta.ID); stored.State != StateRunning {
		t.Fatalf("stored state = %s, want RUNNING", stored.State)
	}
	// fakeRunc는 start 직후 종료하므로 관측 상태는 STOPPED, 그 상태에서의 재시작은 거부된다
	if err := e.rt.Start(ctx, meta.ID); err == nil {
		t.Fatalf("expected error starting a stopped container")
	}
}

func TestMissingContainerLeavesNoLockFile(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	const id = "000000000000"

	if err := e.rt.Start(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("start: expected ErrNotFound, got %v", err)
	}
	if err := e.rt.Stop(ctx, id, time.Second); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stop: expected ErrNotFound, got %v", err)
	}
	if err := e.rt.Delete(ctx, id, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete: expected ErrNotFound, got %v", err)
	}
	e.assertNoTrace(t, id)
}

// 생성 도중 중단되어 레코드만 남은 컨테이너(번들/runc 컨테이너 없음)도 rm으로 정리할 수 있어야 한다
func TestDelete_HalfCreatedContainer(t *testing.T) {
	e := newTestEnv(t)
	const id = "abcdef012345"

	if err := e.store.Put(&ContainerMetadata{ID: id, State: StateCreated}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := e.rt.Delete(context.Background(), id, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	e.assertNoTrace(t, id)
}

func TestResolve_NamePrefixAndAmbiguity(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	for _, m := range []*ContainerMetadata{
		{ID: "c8a19d3f10be", Name: "box-1", State: StateCreated},
		{ID: "c8a1deadbeef", Name: "box-2", State: StateCreated},
	} {
		if err := e.store.Put(m); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	resolves := map[string]string{
		"c8a19d3f10be": "c8a19d3f10be", // 전체 ID
		"box-2":        "c8a1deadbeef", // 이름
		"c8a19d":       "c8a19d3f10be", // 유일한 접두어
	}
	for ref, want := range resolves {
		meta, err := e.rt.Get(ctx, ref)
		if err != nil || meta.ID != want {
			t.Fatalf("Get(%q) = %v, %v; want id %s", ref, meta, err, want)
		}
	}

	// 존재하지 않는 대상 / 경로 조작 / 너무 짧은 접두어는 not found
	for _, ref := range []string{".", "..", "../containers", "non-existent", "c8a", "000000000000"} {
		if err := e.rt.Delete(ctx, ref, true); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Delete(%q): expected ErrNotFound, got %v", ref, err)
		}
	}
	if err := e.rt.Delete(ctx, "", true); err == nil {
		t.Fatalf("expected error for empty reference")
	}

	// 접두어 "c8a1"은 두 컨테이너에 일치 → 임의로 고르지 않고 에러 (not found와는 구분된다)
	if err := e.rt.Delete(ctx, "c8a1", true); err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ambiguity error for 'c8a1', got %v", err)
	}

	// 실패한 요청들은 아무것도 지우지 않았어야 한다
	if list, _ := e.store.List(); len(list) != 2 {
		t.Fatalf("containers were deleted by failed references: %+v", list)
	}

	// 이름으로 삭제하면 해당 컨테이너만 지워진다
	if err := e.rt.Delete(ctx, "box-1", false); err != nil {
		t.Fatalf("delete by name: %v", err)
	}
	if list, _ := e.store.List(); len(list) != 1 || list[0].ID != "c8a1deadbeef" {
		t.Fatalf("unexpected containers after delete: %+v", list)
	}
}

func TestResolve_DuplicateNameIsAmbiguous(t *testing.T) {
	e := newTestEnv(t)
	for _, id := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		if err := e.store.Put(&ContainerMetadata{ID: id, Name: "dup", State: StateCreated}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if err := e.rt.Start(context.Background(), "dup"); err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestDelete_RefusesRunningWithoutForce(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	meta, err := e.rt.Create(ctx, CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	e.runc.mu.Lock()
	e.runc.status[meta.ID] = runc.StatusRunning
	e.runc.mu.Unlock()

	if err := e.rt.Delete(ctx, meta.ID, false); err == nil {
		t.Fatalf("expected error deleting a running container without force")
	}
	if _, err := e.store.Get(meta.ID); err != nil {
		t.Fatalf("record must survive a refused delete: %v", err)
	}
	if err := e.rt.Delete(ctx, meta.ID, true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	e.assertNoTrace(t, meta.ID)
}
