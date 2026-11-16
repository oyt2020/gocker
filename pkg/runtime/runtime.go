package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/oyt2020/gocker/pkg/bundle"
	"github.com/oyt2020/gocker/pkg/oci"
	"github.com/oyt2020/gocker/pkg/reaper"
	"github.com/oyt2020/gocker/pkg/runc"
	"github.com/oyt2020/gocker/pkg/store"
	"github.com/oyt2020/gocker/pkg/util"
)

// ErrNameInUse 같은 이름을 가진 컨테이너가 이미 존재한다 (이름은 컨테이너 사이에서 유일해야 한다)
var ErrNameInUse = errors.New("container name already in use")

// CreateOptions Create 옵션
type CreateOptions struct {
	ID            string // 비어 있으면 런타임이 생성한다
	Name          string // 비어 있지 않으면 다른 컨테이너와 겹칠 수 없다 (ErrNameInUse)
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

// ErrDetachWithRemove 종료 시 자동 삭제(Remove)는 포그라운드 실행에서만 가능하다.
// detach된 컨테이너는 gocker보다 오래 살기 때문에, 컨테이너가 끝났을 때 그것을 삭제해 줄 프로세스가 남아 있지 않다.
var ErrDetachWithRemove = errors.New("remove-on-exit cannot be combined with detach: no process remains to remove the container when it exits")

// RunOptions Run 옵션. 출력 대상은 CreateOptions.Stdout / Stderr를 사용한다.
type RunOptions struct {
	CreateOptions
	Detach bool
	Remove bool      // --rm (종료 시 자동 삭제). Detach와 함께 쓸 수 없다 (ErrDetachWithRemove)
	Stdin  io.Reader // terminal 모드에서 PTY로 전달할 입력 (nil이면 입력 전달 안 함)
}

// RunResult Run 결과
type RunResult struct {
	ID string // 할당된 컨테이너 ID
	// ExitCode 포그라운드 실행이면 컨테이너 프로세스의 종료 코드 (시그널로 죽었으면 128+시그널 번호).
	// detach면 0, 종료 코드를 얻지 못하고 실패하면 -1이다.
	ExitCode int
}

// ContainerRuntime 컨테이너 라이프사이클 관리 인터페이스 (CLI에 대한 API 경계)
// 터미널 제어(raw 모드 등)는 호출자(CLI)의 책임이며, 런타임은 주입받은 스트림만 사용한다.
// ref는 컨테이너 참조(전체 ID / ID 접두어 / 이름)이며, 전체 ID로의 해석은 런타임이 한다 (resolve.go).
type ContainerRuntime interface {
	Create(ctx context.Context, opts CreateOptions) (*ContainerMetadata, error)
	Start(ctx context.Context, ref string) error
	Run(ctx context.Context, opts RunOptions) (RunResult, error)
	Stop(ctx context.Context, ref string, timeout time.Duration) error
	Delete(ctx context.Context, ref string, force bool) error
	List(ctx context.Context, all bool) ([]*ContainerMetadata, error)
	Get(ctx context.Context, ref string) (*ContainerMetadata, error)
}

type runcRuntime struct {
	bundleMgr   bundle.BundleManager
	specBuilder oci.SpecBuilder
	runcClient  runc.RuncClient
	store       store.ContainerStore
	reaper      reaper.Reaper

	stopPollInterval time.Duration // stop에서 종료 여부를 확인하는 주기
	killWaitTimeout  time.Duration // SIGKILL 이후 프로세스가 실제로 사라지기를 기다리는 최대 시간
}

// NewContainerRuntime : ContainerRuntime 생성 (nil 인자는 기본 구현 사용)
func NewContainerRuntime(bm bundle.BundleManager, sb oci.SpecBuilder, rc runc.RuncClient, st store.ContainerStore, rp reaper.Reaper) ContainerRuntime {
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
		st = store.NewFileStore(bm.Layout().StateDir())
	}
	if rp == nil {
		rp = reaper.New()
	}
	return &runcRuntime{
		bundleMgr:   bm,
		specBuilder: sb,
		runcClient:  rc,
		store:       st,
		reaper:      rp,

		stopPollInterval: defaultStopPollInterval,
		killWaitTimeout:  defaultKillWaitTimeout,
	}
}

const (
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

// Create : `runc create` 명령어 실행 + 번들 디렉터리, config.json 준비
func (r *runcRuntime) Create(ctx context.Context, opts CreateOptions) (*ContainerMetadata, error) {
	id, err := ensureID(opts.ID)
	if err != nil {
		return nil, err
	}
	opts.ID = id

	meta, master, err := r.create(ctx, opts)
	if err != nil {
		return nil, err
	}
	if master != nil {
		// gocker는 데몬이 없으므로 master를 유지하지 않는다: 여기서 닫혀 이후 start는 실패한다(의도된 동작)
		_ = master.Close()
	}
	return meta, nil
}

// create : 컨테이너를 CREATED 상태까지 만든다 (opts.ID는 이미 할당되어 있어야 한다).
//
// 순서는 "레코드 → 번들 → runc" 이고 롤백은 그 역순이다. 레코드를 가장 먼저 쓰기 때문에 도중에 프로세스가
// 죽어도 컨테이너가 목록에 보이고 rm으로 정리할 수 있다 (Delete는 없는 번들/runc 컨테이너를 허용한다).
// 전체 과정은 컨테이너 잠금으로 보호되므로, 만들어지는 중인 컨테이너에 대한 start/stop/rm은 완료까지 대기한다.
//
// 롤백 자체가 실패하면 그 에러도 함께 반환한다. 이때 롤백은 실패한 지점에서 멈추므로 레코드가 남고,
// 남은 리소스는 rm으로 다시 정리할 수 있다.
//
// terminal 모드에서는 PTY master를 함께 반환하며 소유권(닫을 책임)은 호출자에게 있다.
func (r *runcRuntime) create(ctx context.Context, opts CreateOptions) (meta *ContainerMetadata, master *os.File, err error) {
	id := opts.ID

	unlock, err := r.store.Lock(id)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()

	if _, err := r.store.Get(id); err == nil {
		return nil, nil, fmt.Errorf("container %q already exists", id)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}

	rollback := NewRollbackTracker()
	defer func() {
		// 성공 경로에서는 Commit으로 비워져 있어 아무것도 실행되지 않는다
		if rbErr := rollback.Rollback(); rbErr != nil {
			err = errors.Join(err, fmt.Errorf("container %s is left behind, remove it with 'gocker rm': %w", id, rbErr))
		}
	}()

	// 1. 메타데이터 레코드 저장 (컨테이너의 존재와 이름을 선언)
	meta = newMetadata(id, opts, r.bundleMgr.Layout().BundleDir(id))
	if err := r.reserve(meta); err != nil {
		return nil, nil, err
	}
	rollback.Add(func() error {
		return r.store.Delete(id)
	})

	// 2. 번들 디렉터리 생성 및 rootfs.tar.gz 압축 해제
	b, err := r.bundleMgr.Create(id, opts.RootfsTarPath)
	if err != nil {
		return nil, nil, fmt.Errorf("create bundle: %w", err)
	}
	rollback.Add(func() error {
		return r.bundleMgr.Delete(id)
	})

	// 3. OCI config.json 생성 및 저장
	spec, err := r.specBuilder.Build(oci.SpecOptions{
		Args:     opts.Command,
		Env:      opts.Env,
		Terminal: opts.Terminal,
		Hostname: opts.Hostname,
		RootFS:   bundle.RootFSDirName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build oci spec: %w", err)
	}
	if err := r.specBuilder.SaveToFile(spec, b.ConfigPath); err != nil {
		return nil, nil, fmt.Errorf("save config.json: %w", err)
	}

	// 4. I/O 옵션 준비
	ioOpts := &runc.IOOption{RuncLogPath: b.RuncLogPath}
	var console runc.Console
	if opts.Terminal {
		// Terminal (PTY) mode: master fd 수신 절차는 runc 계층(Console)이 맡는다.
		// Close는 어떤 반환 경로에서든 안전하며, 넘겨받지 못한 master가 있으면 함께 회수한다.
		console, err = r.runcClient.NewConsole()
		if err != nil {
			return nil, nil, fmt.Errorf("create console: %w", err)
		}
		defer console.Close()
		ioOpts.ConsoleSocket = console.Path()
	} else {
		// Non-terminal mode: 출력 대상이 지정되지 않으면 container.log 파일로 기록
		ioOpts.Stdout = opts.Stdout
		ioOpts.Stderr = opts.Stderr
		if ioOpts.Stdout == nil || ioOpts.Stderr == nil {
			lf, err := os.OpenFile(b.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return nil, nil, fmt.Errorf("open container log file: %w", err)
			}
			defer lf.Close()
			if ioOpts.Stdout == nil {
				ioOpts.Stdout = lf
			}
			if ioOpts.Stderr == nil {
				ioOpts.Stderr = lf
			}
		}
	}

	// 5. `runc create` 호출
	if err := r.runcClient.Create(ctx, id, b.Path, b.PidFilePath, ioOpts); err != nil {
		return nil, nil, fmt.Errorf("runc create: %w", err)
	}
	// runc create 성공 직후 롤백 등록: 이후 어떤 실패 경로에서도 runc 컨테이너가 고아로 남지 않도록 한다.
	rollback.Add(func() error {
		return r.runcClient.Delete(context.Background(), id, true)
	})

	// 6. terminal 모드: runc가 보낸 PTY master 수신
	if console != nil {
		master, err = console.WaitMaster(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("receive pty master: %w", err)
		}
	}

	// Success: 롤백 함수 제거
	rollback.Commit()
	return meta, master, nil
}

// reserve : 새 컨테이너의 레코드를 기록한다.
//
// 이름이 있으면 다른 컨테이너가 이미 쓰고 있는지 확인하고, 쓰고 있으면 ErrNameInUse를 반환한다.
// "중복 검사 → 기록"은 저장소 전역 잠금으로 묶여 있어, 동시에 실행된 다른 gocker 프로세스가 같은 이름을 차지할 수 없다.
// 잠금은 기록 직후 풀리므로 이후의 느린 단계(rootfs 압축 해제, runc create)가 다른 컨테이너의 생성을 막지 않는다.
// 이름은 레코드와 함께 사라지므로, 컨테이너를 삭제하면 그 이름을 다시 쓸 수 있다.
func (r *runcRuntime) reserve(meta *ContainerMetadata) error {
	if meta.Name != "" {
		unlock, err := r.store.LockNames()
		if err != nil {
			return err
		}
		defer unlock()

		existing, err := r.store.List()
		if err != nil {
			return fmt.Errorf("list containers: %w", err)
		}
		for _, other := range existing {
			if other.Name == meta.Name {
				return fmt.Errorf("container name %q is already in use by container %s: %w", meta.Name, other.ID, ErrNameInUse)
			}
		}
	}

	if err := r.store.Put(meta); err != nil {
		return fmt.Errorf("save container metadata: %w", err)
	}
	return nil
}

// start : `runc start` 명령어 실행
func (r *runcRuntime) start(ctx context.Context, id string) error {
	unlock, err := r.store.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	meta, err := r.get(ctx, id)
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

	// 컨테이너 메인 프로세스 PID 가져오기 (읽지 못해도 시작은 성공이며, 조회 시 runc 관측 값으로 채워진다)
	pid, _ := r.initPid(id)

	if err := meta.TransitionTo(StateRunning); err != nil {
		return err
	}
	meta.Pid = pid
	return r.store.Put(meta)
}

// initPid : `runc create`가 pid 파일에 기록한 컨테이너 init 프로세스의 (호스트 기준) PID를 읽는다
func (r *runcRuntime) initPid(id string) (int, error) {
	path := r.bundleMgr.Layout().PidFilePath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid file %s: %q", path, strings.TrimSpace(string(data)))
	}
	return pid, nil
}

// Run : `runc create` + `runc start` 를 동시에 수행
//
// 포그라운드(detach가 아님)에서는 컨테이너가 끝날 때까지 기다려 종료 코드를 반환한다. 이를 위해 gocker가
// child subreaper가 되어 컨테이너 init 프로세스를 입양하고 직접 wait한다 (pkg/reaper 참고).
// detach에서는 gocker가 컨테이너보다 먼저 종료하므로 종료 코드를 수거할 프로세스가 없다 (데몬/shim이 없는 구조의 한계).
//
// 정리(--rm, 시작 실패 후 삭제)가 실패하면 그 에러도 함께 반환한다.
func (r *runcRuntime) Run(ctx context.Context, opts RunOptions) (result RunResult, err error) {
	if opts.Detach && opts.Remove {
		return RunResult{ExitCode: -1}, ErrDetachWithRemove
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
		if _, err := r.Create(ctx, opts.CreateOptions); err != nil {
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
	_, ptyMaster, err := r.create(ctx, createOpts)
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

// waitForExit : 입양한 컨테이너 init 프로세스가 끝날 때까지 기다려 종료 상태를 수거(reap)하고,
// 종료 코드를 레코드에 남긴 뒤 반환한다. 호출 전에 reaper.Adopt가 `runc create`보다 먼저 불려 있어야 한다.
//
// 컨테이너가 이미 끝났어도 수거 전까지는 좀비로 남아 있으므로 종료 코드를 놓치지 않는다.
// 종료 코드를 얻었지만 기록에 실패하면 종료 코드와 에러를 함께 반환한다.
func (r *runcRuntime) waitForExit(ctx context.Context, id string) (int, error) {
	pid, err := r.initPid(id)
	if err != nil {
		return -1, fmt.Errorf("find container process: %w", err)
	}

	exitCode, err := r.reaper.Wait(ctx, pid)
	if err != nil {
		return -1, fmt.Errorf("wait container process: %w", err)
	}

	if err := r.recordExit(id, exitCode); err != nil {
		return exitCode, fmt.Errorf("record exit code: %w", err)
	}
	return exitCode, nil
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

// remove : 컨테이너에 딸린 리소스를 "runc → 번들 → 레코드" 순서로 제거한다.
// 레코드를 마지막에 지우므로 도중에 실패하거나 중단되어도 컨테이너가 목록에 남아 다시 rm할 수 있다.
// 그래서 이미 없는 runc 컨테이너 / 번들은 에러가 아니다 (생성 도중 중단된 컨테이너도 같은 경로로 정리된다).
//
// 반대로 어느 단계든 "없어서"가 아닌 이유로 실패하면 거기서 멈추고 에러를 반환한다. 치우지 못한 리소스를 두고
// 다음 단계로 넘어가면, 살아 있는 runc 컨테이너를 가리키던 번들과 레코드가 사라져 다시 정리할 수 없게 된다.
func (r *runcRuntime) remove(ctx context.Context, id string, force bool) error {
	unlock, err := r.store.Lock(id)
	if err != nil {
		return fmt.Errorf("get container %q: %w", id, err)
	}
	defer unlock() // 레코드가 지워졌으면 잠금 파일도 함께 정리된다

	meta, err := r.get(ctx, id)
	if err != nil {
		return fmt.Errorf("get container %q: %w", id, err)
	}

	if meta.State == StateRunning && !force {
		return fmt.Errorf("container %q is running, use -f to force remove", id)
	}

	// runc 리소스 제거 (이미 없는 것만 정상으로 본다)
	if err := r.runcClient.Delete(ctx, id, force); err != nil && !errors.Is(err, runc.ErrNotFound) {
		return fmt.Errorf("delete runc container: %w", err)
	}

	// 번들 디렉터리 제거
	if err := r.bundleMgr.Delete(id); err != nil {
		return fmt.Errorf("delete bundle: %w", err)
	}

	// 메타데이터 레코드 제거 (이 시점부터 컨테이너는 존재하지 않는다)
	return r.store.Delete(id)
}

// get : 저장된 메타데이터에 runc에서 관측한 실제 상태를 반영하여 반환한다.
// 읽기 전용이다: 관측 결과는 반환값에만 반영되고 저장하지 않는다 (저장은 상태를 바꾸는 명령의 책임).
func (r *runcRuntime) get(ctx context.Context, id string) (*ContainerMetadata, error) {
	meta, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	r.observe(ctx, meta)
	return meta, nil
}

// observe : runc 상태를 조회해 메타데이터에 반영한다 (조회 실패 또는 created/creating 등은 저장된 상태 유지)
func (r *runcRuntime) observe(ctx context.Context, meta *ContainerMetadata) {
	state, err := r.runcClient.State(ctx, meta.ID)
	if err != nil {
		return
	}
	switch state.Status {
	case runc.StatusRunning:
		meta.ApplyObserved(StateRunning, state.Pid)
	case runc.StatusStopped:
		meta.ApplyObserved(StateStopped, 0)
	}
}

// List : 저장소의 모든 컨테이너에 runc 관측 상태를 반영하여 반환한다 (all이 false면 실행 중인 것만)
func (r *runcRuntime) List(ctx context.Context, all bool) ([]*ContainerMetadata, error) {
	metas, err := r.store.List()
	if err != nil {
		return nil, err
	}

	var list []*ContainerMetadata
	for _, meta := range metas {
		r.observe(ctx, meta)
		if all || meta.State == StateRunning {
			list = append(list, meta)
		}
	}
	return list, nil
}
