package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	ctd "github.com/oyt2020/gocker/pkg/containerd"
	"github.com/oyt2020/gocker/pkg/runtime"
)

type mockRuntime struct {
	createdOpts  []runtime.CreateOptions
	startedIDs   []string
	stoppedIDs   []string
	deletedIDs   []string
	runOpts      []runtime.RunOptions
	containers   []*runtime.ContainerMetadata
	startErr     error
	stopErr      error
	deleteErr    error
	createErr    error
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

func (m *mockRuntime) Run(ctx context.Context, opts runtime.RunOptions) (int, error) {
	m.runOpts = append(m.runOpts, opts)
	return 0, nil
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
				ID:        "c8a19d3f10be1234567890ab",
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

	// gocker stop test-box (should resolve name)
	if err := d.Execute(context.Background(), []string{"stop", "test-box"}); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if len(rt.stoppedIDs) != 1 || rt.stoppedIDs[0] != "c8a19d3f10be1234567890ab" {
		t.Fatalf("expected stopped id c8a19d3f10be1234567890ab, got %v", rt.stoppedIDs)
	}

	// gocker start test-box
	if err := d.Execute(context.Background(), []string{"start", "test-box"}); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if len(rt.startedIDs) != 1 || rt.startedIDs[0] != "c8a19d3f10be1234567890ab" {
		t.Fatalf("expected started id c8a19d3f10be1234567890ab, got %v", rt.startedIDs)
	}

	// gocker rm test-box
	if err := d.Execute(context.Background(), []string{"rm", "test-box"}); err != nil {
		t.Fatalf("rm failed: %v", err)
	}
	if len(rt.deletedIDs) != 1 || rt.deletedIDs[0] != "c8a19d3f10be1234567890ab" {
		t.Fatalf("expected deleted id c8a19d3f10be1234567890ab, got %v", rt.deletedIDs)
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
