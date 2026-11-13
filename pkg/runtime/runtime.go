package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/oyt2020/gocker/pkg/bundle"
	"github.com/oyt2020/gocker/pkg/oci"
	"github.com/oyt2020/gocker/pkg/runc"
	"github.com/oyt2020/gocker/pkg/store"
	"github.com/oyt2020/gocker/pkg/util"
)

// CreateOptions Create 옵션
type CreateOptions struct {
	ID            string // 비어 있으면 런타임이 생성한다
	Name          string
	RootfsTarPath string
	Command       []string
	Env           []string
	Terminal      bool
	Hostname      string
	// Stdout / Stderr 컨테이너 출력 대상. nil이면 non-terminal 모드에서는 container.log로 기록된다.
	// runc 자식 프로세스에 직접 연결되므로 *os.File을 권장한다 (파이프 복사 고루틴 / FD 상속 데드락 방지).
	Stdout io.Writer
	Stderr io.Writer
}

// RunOptions Run 옵션. 출력 대상은 CreateOptions.Stdout / Stderr를 사용한다.
type RunOptions struct {
	CreateOptions
	Detach bool
	Remove bool      // --rm
	Stdin  io.Reader // terminal 모드에서 PTY로 전달할 입력 (nil이면 입력 전달 안 함)
}

// RunResult Run 결과
type RunResult struct {
	ID       string // 할당된 컨테이너 ID
	ExitCode int
}

// ContainerRuntime 컨테이너 라이프사이클 관리 인터페이스 (CLI에 대한 API 경계)
// 터미널 제어(raw 모드 등)는 호출자(CLI)의 책임이며, 런타임은 주입받은 스트림만 사용한다.
type ContainerRuntime interface {
	Create(ctx context.Context, opts CreateOptions) (*ContainerMetadata, error)
	Start(ctx context.Context, id string) error
	Run(ctx context.Context, opts RunOptions) (RunResult, error)
	Stop(ctx context.Context, id string, timeout time.Duration) error
	Delete(ctx context.Context, id string, force bool) error
	List(ctx context.Context, all bool) ([]*ContainerMetadata, error)
	Get(ctx context.Context, id string) (*ContainerMetadata, error)
}

type runcRuntime struct {
	bundleMgr   bundle.BundleManager
	specBuilder oci.SpecBuilder
	runcClient  runc.RuncClient
	store       store.ContainerStore
}

// NewContainerRuntime : ContainerRuntime 생성 (nil 인자는 기본 구현 사용)
func NewContainerRuntime(bm bundle.BundleManager, sb oci.SpecBuilder, rc runc.RuncClient, st store.ContainerStore) ContainerRuntime {
	if bm == nil {
		bm = bundle.NewBundleManager(nil)
	}
	if sb == nil {
		sb = oci.NewSpecBuilder()
	}
	if rc == nil {
		rc = runc.NewRuncClient("")
	}
	if st == nil {
		st = store.NewFileStore(bm.Layout())
	}
	return &runcRuntime{
		bundleMgr:   bm,
		specBuilder: sb,
		runcClient:  rc,
		store:       st,
	}
}

const (
	// masterRecvTimeout runc create 이후 PTY master fd 수신을 기다리는 최대 시간
	masterRecvTimeout = 5 * time.Second
	// ptyDrainTimeout 컨테이너 종료 후 PTY 출력이 자연 종료(EIO)되기를 기다리는 최대 시간
	ptyDrainTimeout = 500 * time.Millisecond
)

// ensureID : ID가 비어 있으면 새로 생성한다 (ID 생성은 런타임의 책임)
func ensureID(id string) (string, error) {
	if id != "" {
		return id, nil
	}
	newID, err := util.GenerateID()
	if err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return newID, nil
}

// newMetadata : CREATED 상태의 메타데이터 생성
func newMetadata(id string, opts CreateOptions, bundlePath string) *ContainerMetadata {
	return &ContainerMetadata{
		ID:         id,
		Name:       opts.Name,
		CreatedAt:  time.Now().UTC(),
		Image:      opts.RootfsTarPath,
		Command:    opts.Command,
		BundlePath: bundlePath,
		State:      StateCreated,
	}
}

// masterResult PTY Master FD 수신 결과
type masterResult struct {
	file *os.File
	err  error
}

// startMasterReceiver : consoleSock에서 PTY master fd를 백그라운드로 수신한다.
// 반환되는 drain은 결과를 아직 채널에서 꺼내지 않은 경로(실패/타임아웃)에서만 호출해야 한다.
// 이미 꺼낸 뒤 호출하면 채널이 비어 있어 영구 블로킹된다.
func startMasterReceiver(consoleSock *runc.ConsoleSocket) (<-chan masterResult, func()) {
	ch := make(chan masterResult, 1)
	go func() {
		f, err := consoleSock.ReceiveMaster()
		ch <- masterResult{file: f, err: err}
	}()

	drain := func() {
		// 리스너와 수락된 연결을 함께 닫아 AcceptUnix / ReadMsgUnix 대기 중인 고루틴을 깨운다
		_ = consoleSock.Close()
		// 수신 고루틴이 종료될 때까지 동기적으로 회수하고, 늦게 도착한 fd가 있으면 닫는다
		if res := <-ch; res.file != nil {
			_ = res.file.Close()
		}
	}
	return ch, drain
}

// waitMaster : master fd 수신을 기다린다. ctx 취소 / 타임아웃 시 drain으로 수신 고루틴과 늦게 온 fd를 회수한다.
func waitMaster(ctx context.Context, ch <-chan masterResult, drain func()) (*os.File, error) {
	select {
	case res := <-ch:
		if res.err != nil {
			if res.file != nil {
				_ = res.file.Close()
			}
			return nil, res.err
		}
		return res.file, nil
	case <-ctx.Done():
		drain()
		return nil, ctx.Err()
	case <-time.After(masterRecvTimeout):
		drain()
		return nil, fmt.Errorf("timeout waiting for pty master from runc (%s expired)", masterRecvTimeout)
	}
}

// Create : `runc create` 명령어 실행 + 번들 디렉터리, config.json 준비
func (r *runcRuntime) Create(ctx context.Context, opts CreateOptions) (*ContainerMetadata, error) {
	id, err := ensureID(opts.ID)
	if err != nil {
		return nil, err
	}

	rollback := NewRollbackTracker()
	defer func() {
		_ = rollback.Rollback()
	}()

	// 1. 번들 디렉터리 생성 및 rootfs.tar.gz 압축 해제
	b, err := r.bundleMgr.Create(id, opts.RootfsTarPath)
	if err != nil {
		return nil, fmt.Errorf("create bundle: %w", err)
	}
	rollback.Add(func() error {
		return r.bundleMgr.Delete(id)
	})

	// 2. OCI config.json 생성 및 저장
	spec, err := r.specBuilder.Build(oci.SpecOptions{
		Args:     opts.Command,
		Env:      opts.Env,
		Terminal: opts.Terminal,
		Hostname: opts.Hostname,
		RootFS:   bundle.RootFSDirName,
	})
	if err != nil {
		return nil, fmt.Errorf("build oci spec: %w", err)
	}

	if err := r.specBuilder.SaveToFile(spec, b.ConfigPath); err != nil {
		return nil, fmt.Errorf("save config.json: %w", err)
	}

	// 3. I/O 옵션 준비
	var ioOpts *runc.IOOption
	var logFile *os.File
	var masterCh <-chan masterResult
	// drainMaster : 실패/타임아웃 경로에서 수신 고루틴이 늦게 전달하는 master fd까지 확실히 회수하여 닫는다.
	var drainMaster func()

	if opts.Terminal {
		// Terminal (PTY) mode: Master FD를 위한 콘솔 소켓 생성
		consoleSock, err := runc.NewConsoleSocket("")
		if err != nil {
			return nil, fmt.Errorf("create console socket: %w", err)
		}
		defer consoleSock.Close()
		ioOpts = &runc.IOOption{
			ConsoleSocket: consoleSock.Path(),
			RuncLogPath:   b.RuncLogPath,
		}

		// runc가 create 시 전송하는 PTY master fd를 백그라운드에서 수신 대기 (데드락 및 Broken pipe 방지)
		// 주의: runc create가 완료되기 전에 master fd를 닫으면 커널이 PTY를 hangup(EIO)시키므로 채널로 전달 후 닫는다.
		masterCh, drainMaster = startMasterReceiver(consoleSock)
	} else {
		// Non-terminal mode: 출력 대상이 지정되지 않으면 container.log 파일로 기록
		out := opts.Stdout
		errOut := opts.Stderr
		if out == nil || errOut == nil {
			lf, err := os.OpenFile(b.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return nil, fmt.Errorf("open container log file: %w", err)
			}
			logFile = lf
			if out == nil {
				out = lf
			}
			if errOut == nil {
				errOut = lf
			}
		}
		ioOpts = &runc.IOOption{
			Stdout:      out,
			Stderr:      errOut,
			RuncLogPath: b.RuncLogPath,
		}
	}
	if logFile != nil {
		defer logFile.Close()
	}

	// 4. `runc create` 호출
	if err := r.runcClient.Create(ctx, id, b.Path, b.PidFilePath, ioOpts); err != nil {
		if drainMaster != nil {
			drainMaster()
		}
		return nil, fmt.Errorf("runc create: %w", err)
	}
	// runc create 성공 직후 롤백 등록: 이후 어떤 실패 경로에서도 runc 컨테이너가 고아로 남지 않도록 한다.
	rollback.Add(func() error {
		return r.runcClient.Delete(context.Background(), id, true)
	})

	if masterCh != nil {
		master, err := waitMaster(ctx, masterCh, drainMaster)
		if err != nil {
			return nil, fmt.Errorf("receive pty master during create: %w", err)
		}
		// gocker는 데몬이 없으므로 master를 유지하지 않는다: Create 종료 시 닫혀 이후 start는 실패한다(의도된 동작)
		defer master.Close()
	}

	// 5. metadata.json 저장
	meta := newMetadata(id, opts, b.Path)
	if err := r.store.Put(meta); err != nil {
		return nil, fmt.Errorf("save container metadata: %w", err)
	}

	// Success: 롤백 함수 제거
	rollback.Commit()
	return meta, nil
}

// Start : `runc start` 명령어 실행
func (r *runcRuntime) Start(ctx context.Context, id string) error {
	unlock, err := r.store.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	b, err := r.bundleMgr.Get(id)
	if err != nil {
		return err
	}

	meta, err := r.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}

	if meta.State == StateRunning {
		return fmt.Errorf("container %q is already running", id)
	}
	if !meta.State.CanTransitionTo(StateRunning) {
		return fmt.Errorf("container %q cannot be started from %s state", id, meta.State)
	}

	if err := r.runcClient.Start(ctx, id); err != nil {
		return fmt.Errorf("runc start: %w", err)
	}

	// 컨테이너 메인 프로세스 PID 가져오기
	pid := 0
	if pidData, err := os.ReadFile(b.PidFilePath); err == nil {
		if p, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			pid = p
		}
	}

	if err := meta.TransitionTo(StateRunning); err != nil {
		return err
	}
	meta.Pid = pid
	return r.store.Put(meta)
}

// Run : `runc create` + `runc start` 를 동시에 수행
func (r *runcRuntime) Run(ctx context.Context, opts RunOptions) (RunResult, error) {
	id, err := ensureID(opts.ID)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	opts.ID = id
	// ID는 생성 단계 이전에 할당되므로 실패 시에도 채워질 수 있다 (해당 컨테이너는 롤백으로 정리됨)
	result := RunResult{ID: id, ExitCode: -1}

	if opts.Remove {
		defer func() {
			_ = r.Delete(context.Background(), id, true)
		}()
	}

	// 1. 백그라운드 실행 / 2. 포그라운드 + 가상 터미널 X
	if opts.Detach || !opts.Terminal {
		if _, err := r.Create(ctx, opts.CreateOptions); err != nil {
			return result, fmt.Errorf("create container: %w", err)
		}

		if err := r.Start(ctx, id); err != nil {
			_ = r.Delete(context.Background(), id, true)
			return result, fmt.Errorf("start container: %w", err)
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
func (r *runcRuntime) runWithTerminal(ctx context.Context, opts RunOptions) (int, error) {
	id := opts.ID

	rollback := NewRollbackTracker()
	defer func() {
		_ = rollback.Rollback()
	}()

	// Master FD를 위한 콘솔 소켓
	consoleSock, err := runc.NewConsoleSocket("")
	if err != nil {
		return -1, fmt.Errorf("create console socket: %w", err)
	}
	defer consoleSock.Close()

	// 번들 디렉터리 및 rootfs 준비
	b, err := r.bundleMgr.Create(id, opts.RootfsTarPath)
	if err != nil {
		return -1, fmt.Errorf("create bundle: %w", err)
	}
	rollback.Add(func() error {
		return r.bundleMgr.Delete(id)
	})

	// OCI config.json 생성 및 저장
	spec, err := r.specBuilder.Build(oci.SpecOptions{
		Args:     opts.Command,
		Env:      opts.Env,
		Terminal: true,
		Hostname: opts.Hostname,
		RootFS:   bundle.RootFSDirName,
	})
	if err != nil {
		return -1, fmt.Errorf("build oci spec: %w", err)
	}
	if err := r.specBuilder.SaveToFile(spec, b.ConfigPath); err != nil {
		return -1, fmt.Errorf("save config.json: %w", err)
	}

	// Master FD 백그라운드 수신 시작 (drainMaster는 master를 꺼내기 전 실패 경로에서만 호출)
	masterCh, drainMaster := startMasterReceiver(consoleSock)

	ioOpts := &runc.IOOption{
		ConsoleSocket: consoleSock.Path(),
		RuncLogPath:   b.RuncLogPath,
	}
	if err := r.runcClient.Create(ctx, id, b.Path, b.PidFilePath, ioOpts); err != nil {
		drainMaster()
		return -1, fmt.Errorf("runc create: %w", err)
	}
	rollback.Add(func() error {
		return r.runcClient.Delete(context.Background(), id, true)
	})

	// master 수신 (ctx 취소 / 타임아웃 시 waitMaster 내부에서 drain)
	ptyMaster, err := waitMaster(ctx, masterCh, drainMaster)
	if err != nil {
		return -1, fmt.Errorf("receive pty master: %w", err)
	}
	defer ptyMaster.Close() // 마지막 정리 지점: 실제 close 시 slave 쪽 hangup

	if err := r.store.Put(newMetadata(id, opts.CreateOptions, b.Path)); err != nil {
		return -1, fmt.Errorf("save container metadata: %w", err)
	}

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
	// 모든 반환 경로에서 "출력 중단 → master Close" 순서를 보장하고, 반환 시점에는 출력 고루틴이 끝나 있으므로
	// 호출자가 반환 직후 터미널을 복구해도 출력이 섞이지 않는다.
	defer stopOutput(0)

	// `runc start` 실행
	if err := r.Start(ctx, id); err != nil {
		return -1, fmt.Errorf("start container: %w", err)
	}

	// Start 성공: 롤백 함수 정리 (컨테이너가 RUNNING 상태로 진입함)
	rollback.Commit()

	// 종료 대기
	exitCode, exitErr := r.waitForExit(ctx, id)

	// 컨테이너 종료 후 PTY 버퍼에 남은 출력이 모두 복사되기를(EIO 자연 종료) 우선 기다리고,
	// 시간 내에 끝나지 않으면 read deadline으로 출력 고루틴을 종료시킨다.
	stopOutput(ptyDrainTimeout)

	return exitCode, exitErr
}

// waitForExit 컨테이너 종료 확인 함수
func (r *runcRuntime) waitForExit(ctx context.Context, id string) (int, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-ticker.C:
			state, err := r.runcClient.State(ctx, id)
			if err != nil || state.Status == runc.StatusStopped {
				return 0, nil
			}
		}
	}
}

// Stop : `runc stop` 최초 신호는 SIGTERM 시간 초과하면 SIGKILL 신호 전송
func (r *runcRuntime) Stop(ctx context.Context, id string, timeout time.Duration) error {
	unlock, err := r.store.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	meta, err := r.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}

	if meta.State == StateStopped {
		return nil
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	markStopped := func() error {
		if err := meta.TransitionTo(StateStopped); err != nil {
			return err
		}
		return r.store.Put(meta)
	}

	// Send SIGTERM
	_ = r.runcClient.Kill(ctx, id, "SIGTERM")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := r.runcClient.State(ctx, id)
		if err != nil || state.Status == runc.StatusStopped {
			return markStopped()
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 시간 아웃시 SIGKILL 신호로 재전송
	_ = r.runcClient.Kill(ctx, id, "SIGKILL")
	return markStopped()
}

// Delete runc 관련 리소스(cgroups 등) 제거 및 번들 디렉터리 제거
func (r *runcRuntime) Delete(ctx context.Context, id string, force bool) error {
	// 잠금 파일은 번들 안에 있으므로 번들 삭제와 함께 사라진다 (대기 중이던 다른 프로세스는 이후 조회에서 not found)
	unlock, err := r.store.Lock(id)
	if err != nil {
		return fmt.Errorf("get container %q: %w", id, err)
	}
	defer unlock()

	meta, err := r.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get container %q: %w", id, err)
	}

	if meta.State == StateRunning && !force {
		return fmt.Errorf("container %q is running, use -f to force remove", id)
	}

	// runc 리소스 제거
	_ = r.runcClient.Delete(ctx, id, force)

	// 번들 디렉터리 제거
	return r.bundleMgr.Delete(id)
}

// Get : 저장된 메타데이터에 runc에서 관측한 실제 상태를 반영하여 반환한다.
// 읽기 전용이다: 관측 결과는 반환값에만 반영되고 저장하지 않는다 (저장은 상태를 바꾸는 명령의 책임).
func (r *runcRuntime) Get(ctx context.Context, id string) (*ContainerMetadata, error) {
	meta, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}

	if state, err := r.runcClient.State(ctx, id); err == nil {
		applyObservedState(meta, state)
	}

	return meta, nil
}

// applyObservedState : runc 상태를 컨테이너 상태로 변환하여 반영한다 (created/creating 등은 저장된 상태 유지)
func applyObservedState(meta *ContainerMetadata, state *runc.RuncState) {
	switch state.Status {
	case runc.StatusRunning:
		meta.ApplyObserved(StateRunning, state.Pid)
	case runc.StatusStopped:
		meta.ApplyObserved(StateStopped, 0)
	}
}

// List : 모든 컨테이너에 대해서 Get 수행
func (r *runcRuntime) List(ctx context.Context, all bool) ([]*ContainerMetadata, error) {
	ids, err := r.bundleMgr.List()
	if err != nil {
		return nil, err
	}

	var list []*ContainerMetadata
	for _, id := range ids {
		meta, err := r.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("컨테이너 %q 조회 실패: %w", id, err)
		}
		if all || meta.State == StateRunning {
			list = append(list, meta)
		}
	}

	return list, nil
}
