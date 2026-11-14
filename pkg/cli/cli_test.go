package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	ctd "github.com/oyt2020/gocker/pkg/containerd"
	"github.com/oyt2020/gocker/pkg/image"
	"github.com/oyt2020/gocker/pkg/runtime"
)

type mockRuntime struct {
	createdOpts []runtime.CreateOptions
	startedIDs  []string
	stoppedIDs  []string
	deletedIDs  []string
	runOpts     []runtime.RunOptions
	containers  []*runtime.ContainerMetadata
	startErr    error
	stopErr     error
	deleteErr   error
	createErr   error
}

func (m *mockRuntime) Create(ctx context.Context, opts runtime.CreateOptions) (*runtime.ContainerMetadata, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.createdOpts = append(m.createdOpts, opts)
	meta := &runtime.ContainerMetadata{
		ID:        opts.ID,
		Name:      opts.Name,
		CreatedAt: time.Now().UTC(),
		Command:   opts.Command,
		State:     runtime.StateCreated,
	}
	m.containers = append(m.containers, meta)
	return meta, nil
}

func (m *mockRuntime) Start(ctx context.Context, id string) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.startedIDs = append(m.startedIDs, id)
	for _, c := range m.containers {
		if c.ID == id {
			c.State = runtime.StateRunning
			c.Pid = 1234
		}
	}
	return nil
}

func (m *mockRuntime) Run(ctx context.Context, opts runtime.RunOptions) (runtime.RunResult, error) {
	m.runOpts = append(m.runOpts, opts)
	return runtime.RunResult{ID: "c0ffee000001"}, nil
}

func (m *mockRuntime) Stop(ctx context.Context, id string, timeout time.Duration) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stoppedIDs = append(m.stoppedIDs, id)
	for _, c := range m.containers {
		if c.ID == id {
			c.State = runtime.StateStopped
		}
	}
	return nil
}

func (m *mockRuntime) Delete(ctx context.Context, id string, force bool) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deletedIDs = append(m.deletedIDs, id)
	return nil
}

func (m *mockRuntime) List(ctx context.Context, all bool) ([]*runtime.ContainerMetadata, error) {
	var res []*runtime.ContainerMetadata
	for _, c := range m.containers {
		if all || c.State == runtime.StateRunning {
			res = append(res, c)
		}
	}
	return res, nil
}

func (m *mockRuntime) Get(ctx context.Context, id string) (*runtime.ContainerMetadata, error) {
	for _, c := range m.containers {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, fmt.Errorf("container %q not found", id)
}

func TestDispatcher_UnknownAndHelp(t *testing.T) {
	rt := &mockRuntime{}
	d := NewDispatcher(rt, nil)

	// Empty args
	if err := d.Execute(context.Background(), []string{}); err != nil {
		t.Fatalf("expected nil for empty args, got %v", err)
	}

	// Help flag
	if err := d.Execute(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("expected nil for --help, got %v", err)
	}

	// Unknown command
	if err := d.Execute(context.Background(), []string{"foobar"}); err == nil {
		t.Fatalf("expected error for unknown command, got nil")
	}
}

func TestDispatcher_PsAndLifecycle(t *testing.T) {
	rt := &mockRuntime{
		containers: []*runtime.ContainerMetadata{
			{
				ID:        "c8a19d3f10be",
				Name:      "test-box",
				CreatedAt: time.Now().Add(-10 * time.Second),
				State:     runtime.StateRunning,
				Pid:       100,
				Command:   []string{"echo", "hi"},
			},
		},
	}
	d := NewDispatcher(rt, nil)

	// gocker ps
	if err := d.Execute(context.Background(), []string{"ps"}); err != nil {
		t.Fatalf("ps failed: %v", err)
	}

	// gocker ps -q
	if err := d.Execute(context.Background(), []string{"ps", "-q"}); err != nil {
		t.Fatalf("ps -q failed: %v", err)
	}

	// gocker stop test-box (이름/ID 해석은 런타임의 책임: CLI는 입력을 그대로 넘긴다)
	if err := d.Execute(context.Background(), []string{"stop", "test-box"}); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if len(rt.stoppedIDs) != 1 || rt.stoppedIDs[0] != "test-box" {
		t.Fatalf("expected stop target test-box, got %v", rt.stoppedIDs)
	}

	// gocker start test-box
	if err := d.Execute(context.Background(), []string{"start", "test-box"}); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if len(rt.startedIDs) != 1 || rt.startedIDs[0] != "test-box" {
		t.Fatalf("expected start target test-box, got %v", rt.startedIDs)
	}

	// gocker rm test-box
	if err := d.Execute(context.Background(), []string{"rm", "test-box"}); err != nil {
		t.Fatalf("rm failed: %v", err)
	}
	if len(rt.deletedIDs) != 1 || rt.deletedIDs[0] != "test-box" {
		t.Fatalf("expected rm target test-box, got %v", rt.deletedIDs)
	}
}

// CLI는 컨테이너 참조를 해석하지 않는다: 여러 대상을 순서대로 넘기고, 런타임 에러가 나면 거기서 멈춘다
func TestDispatcher_PassesTargetsThroughAndStopsOnError(t *testing.T) {
	rt := &mockRuntime{}
	d := NewDispatcher(rt, nil)

	if err := d.Execute(context.Background(), []string{"rm", "-f", "c8a1", "box-2", "../etc"}); err != nil {
		t.Fatalf("rm failed: %v", err)
	}
	if got := fmt.Sprint(rt.deletedIDs); got != "[c8a1 box-2 ../etc]" {
		t.Fatalf("targets must reach the runtime unmodified, got %s", got)
	}

	rt.deleteErr = fmt.Errorf("container not found")
	rt.deletedIDs = nil
	if err := d.Execute(context.Background(), []string{"rm", "a", "b"}); err == nil {
		t.Fatalf("expected runtime error to propagate")
	}
	if len(rt.deletedIDs) != 0 {
		t.Fatalf("expected no successful deletions, got %v", rt.deletedIDs)
	}
}

func TestDispatcher_CreateDelegatesIDAndImage(t *testing.T) {
	rt := &mockRuntime{}
	d := NewDispatcher(rt, nil)

	if err := d.Execute(context.Background(), []string{"create", "--name", "box", "alpine", "echo", "hi"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if len(rt.createdOpts) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(rt.createdOpts))
	}
	opts := rt.createdOpts[0]
	if opts.ID != "" {
		t.Fatalf("CLI must not generate container ID, got %q", opts.ID)
	}
	if opts.RootfsTarPath != image.DefaultRootFS {
		t.Fatalf("expected rootfs %q, got %q", image.DefaultRootFS, opts.RootfsTarPath)
	}
	if len(opts.Command) != 2 || opts.Command[0] != "echo" || opts.Command[1] != "hi" {
		t.Fatalf("unexpected command %v", opts.Command)
	}
}

func TestDispatcher_RunStdioPolicy(t *testing.T) {
	rt := &mockRuntime{}
	d := NewDispatcher(rt, nil)

	// detach: 출력 대상을 지정하지 않아야 한다 (런타임이 container.log로 기록)
	if err := d.Execute(context.Background(), []string{"run", "-d", "alpine", "sleep", "1"}); err != nil {
		t.Fatalf("run -d failed: %v", err)
	}
	// foreground: 현재 프로세스의 stdout/stderr로 연결해야 한다
	if err := d.Execute(context.Background(), []string{"run", "--rm", "alpine", "echo", "hi"}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(rt.runOpts) != 2 {
		t.Fatalf("expected 2 run calls, got %d", len(rt.runOpts))
	}
	detached, fg := rt.runOpts[0], rt.runOpts[1]

	if detached.ID != "" || fg.ID != "" {
		t.Fatalf("CLI must not generate container ID")
	}
	if detached.Stdout != nil || detached.Stderr != nil || detached.Stdin != nil {
		t.Fatalf("detached run must not attach stdio, got %+v", detached.CreateOptions)
	}
	if fg.Stdout == nil || fg.Stderr == nil {
		t.Fatalf("foreground run must attach stdout/stderr")
	}
	if fg.Stdin != nil {
		t.Fatalf("non-tty foreground run must not forward stdin")
	}
}

func TestDispatcher_ContainerdSupplierLazy(t *testing.T) {
	rt := &mockRuntime{}
	supplierCalled := false
	supplier := func() (*ctd.Client, error) {
		supplierCalled = true
		return nil, fmt.Errorf("mock error")
	}

	d := NewDispatcher(rt, supplier)

	// Running ps shouldn't call containerd supplier!
	if err := d.Execute(context.Background(), []string{"ps"}); err != nil {
		t.Fatalf("ps failed: %v", err)
	}
	if supplierCalled {
		t.Fatalf("expected supplier not called for ps command")
	}

	// Running pull should trigger supplier
	_ = d.Execute(context.Background(), []string{"pull", "alpine"})
	if !supplierCalled {
		t.Fatalf("expected supplier called for pull command")
	}
}
