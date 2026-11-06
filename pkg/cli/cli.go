package cli

import (
	"context"
	"fmt"

	ctd "github.com/oyt2020/gocker/pkg/containerd"
	"github.com/oyt2020/gocker/pkg/runtime"
)

// Dispatcher CLI 서브커맨드 라우터
type Dispatcher struct {
	rt          runtime.ContainerRuntime    // 새로 생성된 runtime 패키지 클라이언트
	ctdSupplier func() (*ctd.Client, error) // containerd 클라이언트 연결 함수 (containerd.NewClient)
}

// NewDispatcher Dispatcher 생성자
func NewDispatcher(rt runtime.ContainerRuntime, ctdSupplier func() (*ctd.Client, error)) *Dispatcher {
	if ctdSupplier == nil {
		ctdSupplier = func() (*ctd.Client, error) {
			return ctd.NewClient("")
		}
	}
	return &Dispatcher{
		rt:          rt,
		ctdSupplier: ctdSupplier,
	}
}

// Execute 명령행 인자를 파싱하여 해당하는 서브커맨드 핸들러로 라우팅
func (d *Dispatcher) Execute(ctx context.Context, args []string) error {
	if len(args) < 1 {
		d.printUsage()
		return nil
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "create":
		return d.handleCreate(ctx, cmdArgs)
	case "start":
		return d.handleStart(ctx, cmdArgs)
	case "run":
		return d.handleRun(ctx, cmdArgs)
	case "stop":
		return d.handleStop(ctx, cmdArgs)
	case "ps":
		return d.handlePs(ctx, cmdArgs)
	case "rm":
		return d.handleRm(ctx, cmdArgs)
	case "exec":
		return d.handleExec(ctx, cmdArgs)
	case "pull":
		return d.handlePull(ctx, cmdArgs)
	case "images":
		return d.handleImages(ctx, cmdArgs)
	case "version":
		return d.handleVersion(ctx, cmdArgs)
	case "-h", "--help", "help":
		d.printUsage()
		return nil
	default:
		return fmt.Errorf("알 수 없는 명령어 %q. 도움말은 'gocker --help'를 확인하세요", cmd)
	}
}

func (d *Dispatcher) printUsage() {
	fmt.Println("Usage: gocker [COMMAND] [OPTIONS] [ARG...]")
	fmt.Println("\n경량 OCI 컨테이너 엔진 Gocker")
	fmt.Println("\n컨테이너 라이프사이클 관리 (Phase 2 OCI / runc 기반):")
	fmt.Println("  create      RootFS 아카이브로부터 새 OCI 컨테이너 생성")
	fmt.Println("  start       중지된 컨테이너 시작")
	fmt.Println("  run         새 컨테이너에서 명령어 실행")
	fmt.Println("  stop        실행 중인 하나 이상의 컨테이너 중지")
	fmt.Println("  ps          컨테이너 목록 출력")
	fmt.Println("  rm          하나 이상의 컨테이너 및 번들 삭제")
	fmt.Println("\n이미지 및 Task 관리 (Phase 1 containerd 기반):")
	fmt.Println("  pull        원격 레지스트리에서 이미지 다운로드")
	fmt.Println("  images      로컬에 다운로드된 이미지 목록 출력")
	fmt.Println("  exec        실행 중인 컨테이너 내부에서 새로운 프로세스 실행")
	fmt.Println("  version     Gocker 및 런타임 버전 정보 출력")
	fmt.Println("\n특정 명령어에 대한 자세한 사용법은 'gocker <command> -h'를 실행하세요.")
}

func (d *Dispatcher) handleVersion(ctx context.Context, _ []string) error {
	fmt.Println("gocker version: 0.2.0 (Phase 2 - OCI / runc engine)")

	c, err := d.ctdSupplier()
	if err != nil {
		fmt.Println("containerd: 연결되지 않음 (데몬 미실행)")
		return nil
	}
	defer func() {
		_ = c.Close()
	}()

	ver, err := c.Version(ctx)
	if err != nil {
		fmt.Printf("containerd: 버전 조회 실패 (%v)\n", err)
		return nil
	}

	fmt.Printf("containerd version: %s\n", ver.Version)
	fmt.Printf("containerd revision: %s\n", ver.Revision)
	return nil
}
