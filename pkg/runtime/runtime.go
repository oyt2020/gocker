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
	"github.com/oyt2020/gocker/pkg/util"
	"golang.org/x/term"
)

// CreateOptions Create 옵션
type CreateOptions struct {
	ID            string
	Name          string
	RootfsTarPath string
	Command       []string
	Env           []string
	Terminal      bool
	Hostname      string
	Stdout        io.Writer
	Stderr        io.Writer
}

// RunOptions Run 옵션 (CreateOptions 포함됨)
type RunOptions struct {
	CreateOptions
	Detach bool
	Remove bool // --rm
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// ContainerRuntime runc 관리 인터페이스
type ContainerRuntime interface {
	Create(ctx context.Context, opts CreateOptions) (*ContainerMetadata, error)
	Start(ctx context.Context, id string) error
	Run(ctx context.Context, opts RunOptions) (int, error)
	Stop(ctx context.Context, id string, timeout time.Duration) error
	Delete(ctx context.Context, id string, force bool) error
	List(ctx context.Context, all bool) ([]*ContainerMetadata, error)
	Get(ctx context.Context, id string) (*ContainerMetadata, error)
}

type runcRuntime struct {
	bundleMgr   bundle.BundleManager
	specBuilder oci.SpecBuilder
	runcClient  runc.RuncClient
}

// NewContainerRuntime : ContainerRuntime 생성
func NewContainerRuntime(bm bundle.BundleManager, sb oci.SpecBuilder, rc runc.RuncClient) ContainerRuntime {
	if bm == nil {
		bm = bundle.NewBundleManager(nil)
	}
	if sb == nil {
		sb = oci.NewSpecBuilder()
	}
	if rc == nil {
		rc = runc.NewRuncClient("")
	}
	return &runcRuntime{
		bundleMgr:   bm,
		specBuilder: sb,
		runcClient:  rc,
	}
}

// masterResult PTY Master FD 수신 결과
type masterResult struct {
	file *os.File
	err  error
}

// Create : `runc create` 명령어 실행 + 번들 디렉터리, config.json 준비
func (r *runcRuntime) Create(ctx context.Context, opts CreateOptions) (*ContainerMetadata, error) {
	id := opts.ID
	if id == "" {
		newID, err := util.GenerateID()
		if err != nil {
			return nil, fmt.Errorf("generate id: %w", err)
		}
		id = newID
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
	var masterCh chan masterResult
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
		masterCh = make(chan masterResult, 1)
		go func() {
			f, err := consoleSock.ReceiveMaster()
			masterCh <- masterResult{file: f, err: err}
		}()

		drainMaster = func() {
			// 리스너와 수락된 연결을 함께 닫아 AcceptUnix / ReadMsgUnix 대기 중인 고루틴을 깨운다
			_ = consoleSock.Close()
			// 수신 고루틴이 종료될 때까지 동기적으로 회수하고, 늦게 도착한 fd가 있으면 닫는다
			if res := <-masterCh; res.file != nil {
				_ = res.file.Close()
			}
		}
	} else {
		// Non-terminal mode: Stdout, Stderr를 container.log 파일로 지정
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
		var res masterResult
		select {
		case res = <-masterCh:
		case <-ctx.Done():
			drainMaster()
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			drainMaster()
			return nil, fmt.Errorf("timeout waiting for pty master from runc (5s expired)")
		}
		if res.file != nil {
			defer res.file.Close()
		}
		if res.err != nil {
			return nil, fmt.Errorf("receive pty master during create: %w", res.err)
		}
	}

	// 4. metadata.json 저장
	meta := &ContainerMetadata{
		ID:         id,
		Name:       opts.Name,
		CreatedAt:  time.Now().UTC(),
		Image:      opts.RootfsTarPath,
		Command:    opts.Command,
		BundlePath: b.Path,
		State:      StateCreated,
		Pid:        0,
	}

	if err := SaveMetadata(meta, b.MetaPath); err != nil {
		return nil, fmt.Errorf("save container metadata: %w", err)
	}

	// Success: 롤백 함수 제거
	rollback.Commit()
	return meta, nil
}

// Start : `runc start` 명령어 실행
func (r *runcRuntime) Start(ctx context.Context, id string) error {
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

	meta.State = StateRunning
	meta.Pid = pid
	return SaveMetadata(meta, b.MetaPath)
}

// Run : `runc create` + `runc start` 를 동시에 수행
func (r *runcRuntime) Run(ctx context.Context, opts RunOptions) (int, error) {
	if opts.Remove {
		defer func() {
			if opts.ID != "" {
				_ = r.Delete(context.Background(), opts.ID, true)
			}
		}()
	}

	// 1. 백그라운드 실행
	if opts.Detach {
		meta, err := r.Create(ctx, opts.CreateOptions)
		if err != nil {
			return -1, fmt.Errorf("create container: %w", err)
		}
		opts.ID = meta.ID

		if err := r.Start(ctx, meta.ID); err != nil {
			_ = r.Delete(context.Background(), meta.ID, true)
			return -1, fmt.Errorf("start container: %w", err)
		}
		return 0, nil
	}

	// 2. 포그라운드 + 가상 터미널 X
	if !opts.Terminal {
		if opts.CreateOptions.Stdout == nil {
			if opts.Stdout != nil {
				opts.CreateOptions.Stdout = opts.Stdout
			} else {
				opts.CreateOptions.Stdout = os.Stdout
			}
		}
		if opts.CreateOptions.Stderr == nil {
			if opts.Stderr != nil {
				opts.CreateOptions.Stderr = opts.Stderr
			} else {
				opts.CreateOptions.Stderr = os.Stderr
			}
		}

		meta, err := r.Create(ctx, opts.CreateOptions)
		if err != nil {
			return -1, fmt.Errorf("create container: %w", err)
		}
		opts.ID = meta.ID

		if err := r.Start(ctx, meta.ID); err != nil {
			_ = r.Delete(context.Background(), meta.ID, true)
			return -1, fmt.Errorf("start container: %w", err)
		}

		// Wait for process to exit
		return r.waitForExit(ctx, meta.ID)
	}

	// 3. 포그라운드 + 가상 터미널
	id := opts.ID
	if id == "" {
		newID, err := util.GenerateID()
		if err != nil {
			return -1, fmt.Errorf("generate id: %w", err)
		}
		id = newID
		opts.ID = id
		opts.CreateOptions.ID = id
	}

	rollback := NewRollbackTracker()
	defer func() {
		_ = rollback.Rollback()
	}()

	// Master FD를 위한 콘솔 소켓
	consoleSock, err := runc.NewConsoleSocket("")
	if err != nil {
		return -1, fmt.Errorf("create console socket: %w", err)
	}
	defer consoleSock.Close() // defer 1

	// 번들 디렉터리 및 rootfs 준비
	b, err := r.bundleMgr.Create(id, opts.RootfsTarPath)
	if err != nil {
		return -1, fmt.Errorf("create bundle: %w", err)
	}
	rollback.Add(func() error {
		return r.bundleMgr.Delete(id)
	})

	// 2. OCI config.json 생성 및 저장
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

	// masterResult를 수신하는 채널 생성
	masterCh := make(chan masterResult, 1)
	go func() {
		f, err := consoleSock.ReceiveMaster()       // Master FD 수신
		masterCh <- masterResult{file: f, err: err} // masterResult 수신 대기
	}()

	// runc create io 옵션 정의
	ioOpts := &runc.IOOption{
		ConsoleSocket: consoleSock.Path(),
		RuncLogPath:   b.RuncLogPath,
	}
	if err := r.runcClient.Create(ctx, id, b.Path, b.PidFilePath, ioOpts); err != nil {
		return -1, fmt.Errorf("runc create: %w", err)
	}
	rollback.Add(func() error {
		return r.runcClient.Delete(context.Background(), id, true)
	})

	meta := &ContainerMetadata{
		ID:         id,
		Name:       opts.Name,
		CreatedAt:  time.Now().UTC(),
		Image:      opts.RootfsTarPath,
		Command:    opts.Command,
		BundlePath: b.Path,
		State:      StateCreated,
		Pid:        0,
	}
	if err := SaveMetadata(meta, b.MetaPath); err != nil {
		return -1, fmt.Errorf("save container metadata: %w", err)
	}

	// masterCh 수신 대기 (타임아웃 및 컨텍스트 취소 방어)
	var res masterResult
	select {
	case res = <-masterCh:
	case <-ctx.Done():
		return -1, ctx.Err()
	case <-time.After(5 * time.Second):
		return -1, fmt.Errorf("timeout waiting for pty master from runc (5s expired)")
	}
	if res.err != nil {
		return -1, fmt.Errorf("receive pty master: %w", res.err)
	}
	ptyMaster := res.file
	defer ptyMaster.Close()

	// 터미널 raw 모드로 전환
	stdinFd := int(os.Stdin.Fd())
	if term.IsTerminal(stdinFd) {
		oldState, err := term.MakeRaw(stdinFd)
		if err == nil {
			defer term.Restore(stdinFd, oldState)
		} // defer 터미널 표준 모드로 복구
	}

	stdoutDone := make(chan struct{})
	// ptyMaster 참조하여 GC 회수 방지
	go func() {
		_, _ = io.Copy(ptyMaster, os.Stdin)
	}()
	go func() {
		_, _ = io.Copy(os.Stdout, ptyMaster)
		close(stdoutDone)
	}()

	// `runc start` 실행
	if err := r.Start(ctx, id); err != nil {
		return -1, fmt.Errorf("start container: %w", err)
	}

	// Start 성공: 롤백 함수 정리 (컨테이너가 RUNNING 상태로 진입함)
	rollback.Commit()

	// 종료 대기
	exitCode, exitErr := r.waitForExit(ctx, id)

	// 컨테이너 종료 후 PTY 버퍼에 남은 마지막 로그가 os.Stdout으로 모두 복사될 때까지 드레인 대기
	select {
	case <-stdoutDone:
	case <-time.After(500 * time.Millisecond):
	}

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
	b, err := r.bundleMgr.Get(id)
	if err != nil {
		return err
	}

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

	// Send SIGTERM
	_ = r.runcClient.Kill(ctx, id, "SIGTERM")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := r.runcClient.State(ctx, id)
		if err != nil || state.Status == runc.StatusStopped {
			meta.State = StateStopped
			return SaveMetadata(meta, b.MetaPath)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 시간 아웃시 SIGKILL 신호로 재전송
	_ = r.runcClient.Kill(ctx, id, "SIGKILL")
	meta.State = StateStopped
	return SaveMetadata(meta, b.MetaPath)
}

// Delete runc 관련 리소스(cgroups 등) 제거 및 번들 디렉터리 제거
func (r *runcRuntime) Delete(ctx context.Context, id string, force bool) error {
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

// Get 컨테이너 메타데이터 갱신 후 반환
func (r *runcRuntime) Get(ctx context.Context, id string) (*ContainerMetadata, error) {
	b, err := r.bundleMgr.Get(id)
	if err != nil {
		return nil, err
	}

	meta, err := LoadMetadata(b.MetaPath)
	if err != nil {
		return nil, fmt.Errorf("load metadata: %w", err)
	}

	// Sync with runc state if possible
	if state, err := r.runcClient.State(ctx, id); err == nil {
		changed := false
		if state.Status == runc.StatusRunning && (meta.State != StateRunning || meta.Pid != state.Pid) {
			meta.State = StateRunning
			meta.Pid = state.Pid
			changed = true
		} else if state.Status == runc.StatusStopped && meta.State != StateStopped {
			meta.State = StateStopped
			changed = true
		}

		if changed {
			_ = SaveMetadata(meta, b.MetaPath)
		}
	}

	return meta, nil
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
