package oci

import (
	"encoding/json"
	"fmt"
	"os"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// SpecOptions : config.json을 위한 구조체
type SpecOptions struct {
	Args     []string
	Env      []string
	Cwd      string
	Terminal bool
	RootFS   string // Defaults to "rootfs"
	Readonly bool
	Hostname string
}

// SpecBuilder : OCI 런타임 명세 관리 인터페이스 (생성, 저장 등)
type SpecBuilder interface {
	Build(opts SpecOptions) (*specs.Spec, error)
	SaveToFile(spec *specs.Spec, destPath string) error
	LoadFromFile(path string) (*specs.Spec, error)
}

type specBuilder struct{}

// NewSpecBuilder : specBuilder 인스턴스 반환
func NewSpecBuilder() SpecBuilder {
	return &specBuilder{}
}

// Build : OCI 런타임 명세(config.json) 설정 후 생성
func (b *specBuilder) Build(opts SpecOptions) (*specs.Spec, error) {
	rootPath := opts.RootFS
	if rootPath == "" {
		rootPath = "rootfs" // root 필드 기본 값
	}

	cwd := opts.Cwd
	if cwd == "" {
		cwd = "/" // cwd 필드 기본 값
	}

	args := opts.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"} // args 필드 기본 값
	}

	env := opts.Env
	if len(env) == 0 {
		env = []string{ // env 필드 기본 값
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"TERM=xterm",
		}
	}

	hostname := opts.Hostname
	if hostname == "" {
		hostname = "gocker"
	}

	caps := DefaultCapabilities() // 기본 권한 세트 생성

	spec := &specs.Spec{
		Version:  specs.Version,
		Hostname: hostname,
		Root: &specs.Root{
			Path:     rootPath,
			Readonly: opts.Readonly,
		},
		Process: &specs.Process{
			Terminal: opts.Terminal,
			User: specs.User{
				UID: 0,
				GID: 0,
			},
			Args: args,
			Env:  env,
			Cwd:  cwd,
			Capabilities: &specs.LinuxCapabilities{
				Bounding:    caps, // 프로세스가 가질 수 있는 권한의 상한선
				Permitted:   caps, // 프로세스가 활성화할 수 있는 권한
				Inheritable: caps, // 자식에게 줄 수 있는 권한
				Effective:   caps, // 커널이 실제로 확인하는 권한
				Ambient:     caps, // 비특권 사용자가 실행한 프로세스가 유지하는 권한
			},
			Rlimits: []specs.POSIXRlimit{
				{
					Type: "RLIMIT_NOFILE", // 파일 디스크립터 번호 범위 제한
					Hard: 1024,            // 최대 1024
					Soft: 1024,            // 현재 적용 1024
				},
			},
			NoNewPrivileges: true, // execve()를 통해 추가 권한 획득 방지
		},
		Mounts: DefaultMounts(), // 기본 마운트 정보 설정
		Linux: &specs.Linux{
			Namespaces: DefaultNamespaces(), // 기본 네임스페이스 정보 설정
			MaskedPaths: []string{ // 제한 파일
				"/proc/kcore",                   // 호스트 물리 메모리
				"/proc/latency_stats",           // 시스템 전체 지연 시간 통계
				"/proc/timer_list",              // 타이머 목록
				"/proc/timer_stats",             // 타이머 이벤트 정보
				"/proc/sched_debug",             // 커널 스케줄러 디버그 정보
				"/sys/firmware",                 // UEFI, ACPI, DMI 등 저수준 펌웨어 인터페이스
				"/proc/acpi",                    // 시스템 전원 관리 인터페이스
				"/proc/keys",                    // 커널 키 관리 서브시스템 목록
				"/proc/scsi",                    // 호스트에 장착된 물리 디바이스 목록
				"/sys/devices/virtual/powercap", // CPU 전력 관련 인터페이스
			},
			ReadonlyPaths: []string{ // 읽기 전용
				"/proc/asound",        // 오디오 장치 설정 및 정보
				"/proc/bus",           // 호스트 PCI, USB 등 시스템 버스 장치 정보
				"/proc/fs",            // 마운트된 파일시스템의 통계
				"/proc/irq",           // 시스템 하드웨어 인터럽트 경로
				"/proc/sys",           // 커널 매개변수 설정 디렉터리
				"/proc/sysrq-trigger", // 키보드 명령을 소프트웨어로 트리거하는 파일
			},
		},
	}

	return spec, nil
}

// SaveToFile : 실제 json 파일로 저장
func (b *specBuilder) SaveToFile(spec *specs.Spec, destPath string) error {
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal OCI spec: %w", err)
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("write OCI config file to %s: %w", destPath, err)
	}

	return nil
}

// LoadFromFile : config.json으로 만든 OCI Spec 구조체
func (b *specBuilder) LoadFromFile(path string) (*specs.Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OCI config file %s: %w", path, err)
	}

	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal OCI config %s: %w", path, err)
	}

	return &spec, nil
}
