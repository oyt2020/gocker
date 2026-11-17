package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/oyt2020/gocker/pkg/store"
)

// ErrDetachWithRemove 종료 시 자동 삭제(Remove)는 포그라운드 실행에서만 가능하다.
// detach된 컨테이너는 gocker보다 오래 살기 때문에, 컨테이너가 끝났을 때 그것을 삭제해 줄 프로세스가 남아 있지 않다.
var ErrDetachWithRemove = errors.New("remove-on-exit cannot be combined with detach: no process remains to remove the container when it exits")

// ErrStdinNotFile 가상 터미널 없이 표준 입력을 연결하려면 *os.File이어야 한다.
// 파일이 아닌 Reader는 os/exec가 파이프와 복사 고루틴으로 중계하는데, 그러면 `runc create`가 입력이 끝날 때까지 반환하지 않는다.
var ErrStdinNotFile = errors.New("stdin must be an *os.File when no terminal is allocated")

// RunOptions Run 옵션. 출력 대상은 CreateOptions.Stdout / Stderr를 사용한다.
type RunOptions struct {
	CreateOptions
	Detach bool
	Remove bool // --rm (종료 시 자동 삭제). Detach와 함께 쓸 수 없다 (ErrDetachWithRemove)
	// Stdin 컨테이너에 전달할 입력 (nil이면 전달하지 않고, 컨테이너의 stdin은 /dev/null이 된다)
	//   - terminal 모드   : PTY master로 복사한다
	//   - terminal 아님   : 컨테이너 프로세스의 stdin에 직접 연결한다. *os.File이어야 한다 (ErrStdinNotFile)
	//   - detach          : 무시한다. 컨테이너가 gocker보다 오래 살고, 나중에 입력을 붙일 수단(attach)이 없다
	Stdin io.Reader
}

// RunResult Run 결과
type RunResult struct {
	ID string // 할당된 컨테이너 ID
	// ExitCode 포그라운드 실행이면 컨테이너 프로세스의 종료 코드 (시그널로 죽었으면 128+시그널 번호).
	// detach면 0, 종료 코드를 얻지 못하고 실패하면 -1이다.
	ExitCode int
}

const (
	// defaultCancelStopTimeout 포그라운드 실행이 취소되었을 때 SIGTERM 이후 종료를 기다리는 기본 시간.
	// 사용자가 Ctrl+C를 누르고 기다리는 상황이므로 stop의 기본 타임아웃(10초)보다 짧게 잡는다.
	defaultCancelStopTimeout = 2 * time.Second

	// ptyDrainTimeout 컨테이너 종료 후 PTY 출력이 자연 종료(EIO)되기를 기다리는 최대 시간
	ptyDrainTimeout = 500 * time.Millisecond
)

// Run : `runc create` + `runc start` 를 동시에 수행
//
// 포그라운드(detach가 아님)에서는 컨테이너가 끝날 때까지 기다려 종료 코드를 반환한다. 이를 위해 gocker가
// child subreaper가 되어 컨테이너 init 프로세스를 입양하고 직접 wait한다 (pkg/reaper 참고).
// detach에서는 gocker가 컨테이너보다 먼저 종료하므로 종료 코드를 수거할 프로세스가 없다 (데몬/shim이 없는 구조의 한계).
//
// 포그라운드 실행 중 ctx가 취소되면 컨테이너를 SIGTERM → (대기) → SIGKILL 순으로 끝낸 뒤 그 종료 코드를 반환한다
// (waitForExit 참고). 컨테이너를 남겨 둔 채 반환하지 않는다.
//
// 정리(--rm, 시작 실패 후 삭제)가 실패하면 그 에러도 함께 반환한다.
func (r *runcRuntime) Run(ctx context.Context, opts RunOptions) (result RunResult, err error) {
	if opts.Detach && opts.Remove {
		return RunResult{ExitCode: -1}, ErrDetachWithRemove
	}

	// 포그라운드 + 가상 터미널 X 에서 컨테이너에 직접 연결할 표준 입력 (아무것도 만들기 전에 검증한다)
	var stdin *os.File
	if !opts.Detach && !opts.Terminal && opts.Stdin != nil {
		f, ok := opts.Stdin.(*os.File)
		if !ok {
			return RunResult{ExitCode: -1}, ErrStdinNotFile
		}
		stdin = f
	}

	id, err := ensureID(opts.ID)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	opts.ID = id
	// ID는 생성 단계 이전에 할당되므로 실패 시에도 채워질 수 있다 (해당 컨테이너는 롤백으로 정리됨)
	result = RunResult{ID: id, ExitCode: -1}

	if !opts.Detach {
		// 입양은 부모(runc create)가 죽는 시점에 결정되므로 반드시 runc create 이전에 subreaper가 되어야 한다
		if err := r.reaper.Adopt(); err != nil {
			return result, fmt.Errorf("become child subreaper: %w", err)
		}
	}

	if opts.Remove {
		defer func() {
			if cErr := r.cleanup(id); cErr != nil {
				err = errors.Join(err, cErr)
			}
		}()
	}

	// 1. 백그라운드 실행 / 2. 포그라운드 + 가상 터미널 X
	if opts.Detach || !opts.Terminal {
		if opts.Detach {
			_, err = r.Create(ctx, opts.CreateOptions)
		} else {
			// 포그라운드 + 가상 터미널 X: master가 없으므로 닫을 것도 없다
			_, _, err = r.create(ctx, opts.CreateOptions, stdin)
		}
		if err != nil {
			return result, fmt.Errorf("create container: %w", err)
		}

		if err := r.start(ctx, id); err != nil {
			err = fmt.Errorf("start container: %w", err)
			if cErr := r.cleanup(id); cErr != nil {
				err = errors.Join(err, cErr)
			}
			return result, err
		}

		if opts.Detach {
			result.ExitCode = 0
			return result, nil
		}

		result.ExitCode, err = r.waitForExit(ctx, id)
		return result, err
	}

	// 3. 포그라운드 + 가상 터미널
	result.ExitCode, err = r.runWithTerminal(ctx, opts)
	return result, err
}

// runWithTerminal : PTY master를 유지한 채 컨테이너를 실행하고 주입받은 스트림과 연결한다.
// 터미널 raw 모드 전환/복구는 호출자의 책임이며, 이 함수는 반환 전에 출력 복사 고루틴의 종료를 보장한다.
func (r *runcRuntime) runWithTerminal(ctx context.Context, opts RunOptions) (exitCode int, err error) {
	id := opts.ID

	createOpts := opts.CreateOptions
	createOpts.Terminal = true
	_, ptyMaster, err := r.create(ctx, createOpts, nil) // 입력은 아래에서 PTY master로 복사한다
	if err != nil {
		return -1, fmt.Errorf("create container: %w", err)
	}

	// Create와 달리 master를 컨테이너가 끝날 때까지 유지한다. 아래 defer들은 등록의 역순으로 실행되어
	// "출력 중단 → master Close → (start 실패 시) 컨테이너 삭제" 순서를 보장한다.
	started := false
	defer func() {
		if !started {
			if cErr := r.cleanup(id); cErr != nil {
				err = errors.Join(err, cErr)
			}
		}
	}()
	defer ptyMaster.Close() // 마지막 정리 지점: 실제 close 시 slave 쪽 hangup

	// 주입받은 스트림과 PTY master 연결
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	if opts.Stdin != nil {
		go func() {
			_, _ = io.Copy(ptyMaster, opts.Stdin)
		}()
	}
	stdoutDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, ptyMaster)
		close(stdoutDone)
	}()

	// stopOutput : 출력 고루틴 정리. grace 동안 자연 종료(EIO)를 기다리고, 끝나지 않으면 read deadline으로
	// 대기 중인 Read를 깨운 뒤 종료를 확인한다. 이미 끝났으면 즉시 반환하므로 여러 번 호출해도 안전하다.
	stopOutput := func(grace time.Duration) {
		select {
		case <-stdoutDone:
			return
		case <-time.After(grace):
		}
		if err := ptyMaster.SetReadDeadline(time.Now()); err == nil {
			<-stdoutDone
		}
		// deadline 설정 실패(non-pollable fd)면 Read를 깨울 수 없으므로 무한 대기하지 않고 진행한다
	}
	// 반환 시점에는 출력 고루틴이 끝나 있으므로 호출자가 반환 직후 터미널을 복구해도 출력이 섞이지 않는다.
	defer stopOutput(0)

	// `runc start` 실행
	if err := r.start(ctx, id); err != nil {
		return -1, fmt.Errorf("start container: %w", err)
	}
	started = true

	// 종료 대기
	exitCode, exitErr := r.waitForExit(ctx, id)

	// 컨테이너 종료 후 PTY 버퍼에 남은 출력이 모두 복사되기를(EIO 자연 종료) 우선 기다리고,
	// 시간 내에 끝나지 않으면 read deadline으로 출력 고루틴을 종료시킨다.
	stopOutput(ptyDrainTimeout)

	return exitCode, exitErr
}

// exitResult 컨테이너 init 프로세스의 wait 결과
type exitResult struct {
	exitCode int
	err      error
}

// waitForExit : 입양한 컨테이너 init 프로세스가 끝날 때까지 기다려 종료 상태를 수거(reap)하고,
// 종료 코드를 레코드에 남긴 뒤 반환한다. 호출 전에 reaper.Adopt가 `runc create`보다 먼저 불려 있어야 한다.
//
// 컨테이너가 이미 끝났어도 수거 전까지는 좀비로 남아 있으므로 종료 코드를 놓치지 않는다.
// 종료 코드를 얻었지만 기록에 실패하면 종료 코드와 에러를 함께 반환한다.
//
// ctx가 취소되면(gocker가 SIGINT / SIGTERM을 받은 경우 등) 기다리기를 그만두는 대신 컨테이너를 끝낸다 (terminate).
// 포그라운드 컨테이너는 gocker와 수명을 같이한다: 그대로 반환하면 컨테이너가 돌볼 프로세스 없이 남고 종료 코드도 잃는다.
// 이 경우에도 반환값은 컨테이너의 실제 종료 코드이며, 취소 자체는 에러로 보고하지 않는다.
func (r *runcRuntime) waitForExit(ctx context.Context, id string) (int, error) {
	pid, err := r.initPid(id)
	if err != nil {
		return -1, fmt.Errorf("find container process: %w", err)
	}

	// wait는 ctx와 무관하게 끝까지 유지한다. 종료 상태는 한 번만 수거할 수 있으므로, 취소되었다고 wait를 버리면
	// 그 뒤에 컨테이너를 멈추더라도 종료 코드를 얻을 방법이 없다.
	exited := make(chan exitResult, 1)
	go func() {
		code, err := r.reaper.Wait(context.Background(), pid)
		exited <- exitResult{code, err}
	}()

	var res exitResult
	select {
	case res = <-exited:
	case <-ctx.Done():
		res = r.terminate(id, exited)
	}
	if res.err != nil {
		return -1, fmt.Errorf("wait container process: %w", res.err)
	}

	if err := r.recordExit(id, res.exitCode); err != nil {
		return res.exitCode, fmt.Errorf("record exit code: %w", err)
	}
	return res.exitCode, nil
}

// terminate : 취소된 포그라운드 실행의 컨테이너를 끝내고 wait 결과를 반환한다.
// stop과 같은 절차다: SIGTERM을 보내 스스로 정리할 기회를 주고, cancelStopTimeout 안에 끝나지 않으면 SIGKILL을 보낸다.
//
// stop과 달리 runc 상태를 폴링하지 않는다. gocker가 컨테이너의 부모이므로 exited(wait 결과)가 종료 순간을 정확히 알려준다.
// 호출자의 ctx는 이미 취소되어 runc 명령에 쓸 수 없으므로 별도의 컨텍스트를 쓴다.
func (r *runcRuntime) terminate(id string, exited <-chan exitResult) exitResult {
	ctx := context.Background()

	// 신호 전송이 실패해도 멈추지 않는다: SIGTERM을 못 보냈으면 바로 SIGKILL로 넘어가고,
	// 끝내 종료를 확인하지 못했을 때 그 원인으로 함께 보고한다.
	termErr := r.signal(ctx, id, "SIGTERM")
	if termErr == nil {
		select {
		case res := <-exited:
			return res
		case <-time.After(r.cancelStopTimeout):
		}
	}

	killErr := r.signal(ctx, id, "SIGKILL")
	select {
	case res := <-exited:
		return res
	case <-time.After(r.killWaitTimeout):
		return exitResult{-1, errors.Join(
			fmt.Errorf("container %q is still running %s after SIGKILL", id, r.killWaitTimeout),
			termErr, killErr,
		)}
	}
}

// recordExit : 수거한 종료 코드를 레코드에 남기고 STOPPED로 전환한다.
// 기다리는 사이 다른 gocker 프로세스가 컨테이너를 삭제했다면 남길 곳이 없으므로 아무것도 하지 않는다.
func (r *runcRuntime) recordExit(id string, exitCode int) error {
	unlock, err := r.store.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	meta, err := r.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	// RUNNING → STOPPED. 그 사이 stop이 먼저 STOPPED로 바꿨어도(같은 상태로의 전환) 종료 코드는 채운다.
	if err := meta.TransitionTo(StateStopped); err != nil {
		return err
	}
	meta.ExitCode = &exitCode
	return r.store.Put(meta)
}

// cleanup : 실패 경로 / --rm 에서 컨테이너를 강제 삭제한다. 이미 없으면(롤백 등으로 정리됨) 할 일이 없다.
// 호출자의 ctx가 취소된 뒤에도 정리는 끝까지 해야 하므로 별도의 컨텍스트를 쓴다.
func (r *runcRuntime) cleanup(id string) error {
	err := r.remove(context.Background(), id, true)
	if err == nil || errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("clean up container %s: %w", id, err)
}
