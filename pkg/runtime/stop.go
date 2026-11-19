package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oyt2020/gocker/pkg/runc"
)

const (
	// defaultStopTimeout stop에서 SIGTERM 이후 종료를 기다리는 기본 시간
	defaultStopTimeout = 10 * time.Second
	// defaultStopPollInterval stop에서 종료 여부를 확인하는 기본 주기
	defaultStopPollInterval = 200 * time.Millisecond
	// defaultKillWaitTimeout SIGKILL 이후 종료를 기다리는 기본 시간. SIGKILL은 무시할 수 없으므로
	// 정상이라면 즉시 끝난다. 이 시간이 지나도 살아 있으면 무언가 잘못된 것이므로 에러로 보고한다.
	defaultKillWaitTimeout = 5 * time.Second
)

// stop : SIGTERM을 보내고 timeout 동안 종료를 기다린 뒤, 끝나지 않으면 SIGKILL을 보내고 종료를 확인한다.
//
// STOPPED는 "실제로 종료된 것을 확인한 경우"에만 기록한다. 종료를 확인하지 못한 채 반환하는 경로
// (ctx 취소/만료, runc 조회 실패, 신호 전송 실패, SIGKILL 이후에도 살아 있음)는 모두 에러를 반환하고
// 레코드를 건드리지 않는다. 이때 신호가 이미 전달되었을 수 있지만, 실제 상태는 다음 조회에서 runc 관측으로 반영된다.
func (r *runcRuntime) stop(ctx context.Context, id string, timeout time.Duration) error {
	unlock, err := r.store.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	meta, err := r.store.Get(id)
	if err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}

	if timeout <= 0 {
		timeout = defaultStopTimeout
	}

	markStopped := func() error {
		if err := meta.TransitionTo(StateStopped); err != nil {
			return err
		}
		return r.store.Put(meta)
	}

	// 이미 끝난 컨테이너(스스로 종료 등)에는 신호를 보내지 않고 기록만 맞춘다.
	// 저장된 상태가 아니라 runc 관측을 기준으로 하므로, 저장은 STOPPED인데 실제로는 살아 있는 컨테이너도 중지된다.
	stopped, err := r.isStopped(ctx, id)
	if err != nil {
		return err
	}
	if stopped {
		return markStopped()
	}

	// 1. SIGTERM 전송 후 timeout 동안 종료 대기
	if err := r.signal(ctx, id, "SIGTERM"); err != nil {
		return err
	}
	stopped, err = r.waitStopped(ctx, id, timeout)
	if err != nil {
		return err
	}
	if stopped {
		return markStopped()
	}

	// 2. 시간 초과: SIGKILL 전송 후 실제로 종료되었는지 확인
	if err := r.signal(ctx, id, "SIGKILL"); err != nil {
		return err
	}
	stopped, err = r.waitStopped(ctx, id, r.killWaitTimeout)
	if err != nil {
		return err
	}
	if !stopped {
		return fmt.Errorf("container %q is still running %s after SIGKILL", id, r.killWaitTimeout)
	}
	return markStopped()
}

// isStopped : runc 관측 기준으로 컨테이너 프로세스가 종료되었는지 확인한다.
//
// 조회 실패를 "종료됨"으로 간주하지 않는다. 종료로 인정하는 실패는 runc 컨테이너가 아예 없는 경우
// (runc.ErrNotFound: 프로세스도 없다)뿐이고, ctx 취소/만료나 그 밖의 실패는 상태를 알 수 없으므로 에러로 돌려준다.
func (r *runcRuntime) isStopped(ctx context.Context, id string) (bool, error) {
	state, err := r.runcClient.State(ctx, id)
	switch {
	case err == nil:
		return state.Status == runc.StatusStopped, nil
	case ctx.Err() != nil:
		return false, ctx.Err()
	case errors.Is(err, runc.ErrNotFound):
		return true, nil
	default:
		return false, fmt.Errorf("check container state: %w", err)
	}
}

// signal : 컨테이너 init 프로세스에 신호를 보낸다.
// 보내기 직전에 컨테이너가 스스로 종료해 보낼 대상이 없어진 경우는 실패가 아니다 (이어지는 종료 확인에서 드러난다).
func (r *runcRuntime) signal(ctx context.Context, id, sig string) error {
	err := r.runcClient.Kill(ctx, id, sig)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if stopped, stateErr := r.isStopped(ctx, id); stateErr == nil && stopped {
		return nil
	}
	return fmt.Errorf("send %s: %w", sig, err)
}

// waitStopped : 컨테이너가 종료될 때까지 최대 timeout 동안 주기적으로 확인한다.
// 종료를 확인하면 true, 시간 내에 끝나지 않으면 false를 반환한다. ctx가 취소/만료되면 즉시 ctx.Err()를 반환한다.
func (r *runcRuntime) waitStopped(ctx context.Context, id string, timeout time.Duration) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(r.stopPollInterval)
	defer ticker.Stop()

	for {
		stopped, err := r.isStopped(ctx, id)
		if err != nil || stopped {
			return stopped, err
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}
