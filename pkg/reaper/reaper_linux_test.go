//go:build linux

package reaper

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func waitWithTimeout(t *testing.T, pid int) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return New().Wait(ctx, pid)
}

func TestWait_ExitCode(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	code, err := waitWithTimeout(t, cmd.Process.Pid)
	if err != nil || code != 7 {
		t.Fatalf("Wait = %d, %v; want 7", code, err)
	}
}

func TestWait_SignaledIs128PlusSignal(t *testing.T) {
	cmd := exec.Command("sh", "-c", "kill -TERM $$")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	code, err := waitWithTimeout(t, cmd.Process.Pid)
	if err != nil || code != 128+15 {
		t.Fatalf("Wait = %d, %v; want 143", code, err)
	}
}

// runc create와 같은 상황: 부모(sh)가 먼저 종료해 고아가 된 손자 프로세스를 입양하고 종료 코드를 수거한다
func TestAdopt_ReapsOrphanedGrandchild(t *testing.T) {
	r := New()
	if err := r.Adopt(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	// sh는 백그라운드 프로세스의 PID만 출력하고 곧바로 종료한다 (손자의 출력은 파이프에서 분리)
	out, err := exec.Command("sh", "-c", "(sleep 0.3; exit 9) >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse pid %q: %v", out, err)
	}

	code, err := waitWithTimeout(t, pid)
	if err != nil || code != 9 {
		t.Fatalf("Wait(orphan) = %d, %v; want 9", code, err)
	}
}

func TestWait_NotAChild(t *testing.T) {
	if _, err := waitWithTimeout(t, 1); err == nil {
		t.Fatalf("expected error waiting on a non-child process")
	}
	if _, err := waitWithTimeout(t, 0); err == nil {
		t.Fatalf("expected error for invalid pid")
	}
}

func TestWait_ContextCancel(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := New().Wait(ctx, cmd.Process.Pid); err == nil {
		t.Fatalf("expected context error")
	}
}
