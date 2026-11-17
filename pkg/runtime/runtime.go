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

	meta, master, err := r.create(ctx, opts, nil)
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
//
// stdin이 nil이 아니면 (terminal 모드가 아닐 때) 컨테이너 프로세스의 표준 입력으로 직접 연결한다.
// 복사 고루틴 없이 fd를 그대로 물려주므로, 입력의 끝(EOF)은 쓰는 쪽이 닫을 때 컨테이너에 자연스럽게 전달된다.
func (r *runcRuntime) create(ctx context.Context, opts CreateOptions, stdin *os.File) (meta *ContainerMetadata, master *os.File, err error) {
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
		// Non-terminal mode: 입력은 지정된 경우에만 연결 (없으면 runc가 /dev/null을 물려준다).
		// nil *os.File을 인터페이스에 그대로 넣으면 "nil이 아닌 인터페이스"가 되므로 반드시 nil 검사 후 대입한다.
		if stdin != nil {
			ioOpts.Stdin = stdin
		}
		// 출력 대상이 지정되지 않으면 container.log 파일로 기록
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
//
// 예외: 저장된 상태는 RUNNING인데 runc가 그 컨테이너를 아예 모르면(runc.ErrNotFound) STOPPED로 본다.
// 재부팅으로 runc 상태 디렉터리(/run/runc, tmpfs)가 비워지면 프로세스는 이미 없는데 레코드만 RUNNING으로 남는다.
// 이것을 그대로 믿으면 죽은 컨테이너가 계속 실행 중으로 보이고 rm이 -f를 요구하게 된다.
// CREATED에는 적용하지 않는다: 생성 중에는 레코드가 runc 컨테이너보다 먼저 만들어지므로 "runc가 모름"이 정상이다.
func (r *runcRuntime) observe(ctx context.Context, meta *ContainerMetadata) {
	state, err := r.runcClient.State(ctx, meta.ID)
	if err != nil {
		if errors.Is(err, runc.ErrNotFound) && meta.State == StateRunning {
			meta.ApplyObserved(StateStopped, 0)
		}
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
