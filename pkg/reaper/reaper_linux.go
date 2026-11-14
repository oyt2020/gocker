//go:build linux

package reaper

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// signalExitBase 시그널로 종료된 프로세스의 종료 코드 기준값 (128 + 시그널 번호)
const signalExitBase = 128

type subreaper struct{}

// New : prctl(PR_SET_CHILD_SUBREAPER) + wait4 기반 Reaper 생성
func New() Reaper {
	return subreaper{}
}

// Adopt : subreaper 속성은 스레드가 아닌 프로세스 단위로 적용되므로 어느 고루틴에서 불러도 된다.
func (subreaper) Adopt() error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_CHILD_SUBREAPER): %w", err)
	}
	return nil
}

type waitResult struct {
	exitCode int
	err      error
}

// Wait : wait4(pid)로 종료를 기다린다. 특정 pid만 기다리므로 os/exec가 실행한 다른 자식(runc 명령)의
// 종료 상태를 가로채지 않는다.
//
// ctx가 취소되면 즉시 반환하지만, 블로킹 중인 wait4는 깨울 수 없어 대기 고루틴은 프로세스가 끝날 때까지 남는다.
func (subreaper) Wait(ctx context.Context, pid int) (int, error) {
	if pid <= 0 {
		return -1, fmt.Errorf("invalid pid %d", pid)
	}

	ch := make(chan waitResult, 1)
	go func() {
		var ws unix.WaitStatus
		for {
			_, err := unix.Wait4(pid, &ws, 0, nil)
			if errors.Is(err, unix.EINTR) {
				continue // Go 런타임의 시그널(선점 등)로 중단될 수 있다
			}
			if errors.Is(err, unix.ECHILD) {
				ch <- waitResult{-1, fmt.Errorf("wait4(%d): %w (process is not a child of gocker: not adopted, or already reaped)", pid, err)}
				return
			}
			if err != nil {
				ch <- waitResult{-1, fmt.Errorf("wait4(%d): %w", pid, err)}
				return
			}
			ch <- waitResult{exitCodeOf(ws), nil}
			return
		}
	}()

	select {
	case res := <-ch:
		return res.exitCode, res.err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// exitCodeOf : wait 상태를 종료 코드로 변환한다
func exitCodeOf(ws unix.WaitStatus) int {
	if ws.Signaled() {
		return signalExitBase + int(ws.Signal())
	}
	return ws.ExitStatus()
}
