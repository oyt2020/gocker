package oci

import (
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// DefaultNamespaces : config.json 기본 네임스페이스 설정 (격리 자원)
func DefaultNamespaces() []specs.LinuxNamespace {
	return []specs.LinuxNamespace{
		{Type: specs.PIDNamespace},     // PID
		{Type: specs.NetworkNamespace}, // Network
		{Type: specs.IPCNamespace},     // IPC
		{Type: specs.UTSNamespace},     // UTS
		{Type: specs.MountNamespace},   // Mount
	}
}

// DefaultMounts : config.json 기본 마운트 설정 (컨테이너 내부 파일 시스템 구성)
func DefaultMounts() []specs.Mount {
	return []specs.Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
		{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"},
		},
		{
			Destination: "/dev/shm",
			Type:        "tmpfs",
			Source:      "shm",
			Options:     []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"},
		},
		{
			Destination: "/dev/mqueue",
			Type:        "mqueue",
			Source:      "mqueue",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
	}
}

// DefaultCapabilities : 컨테이너에 실행되는 프로세스의 권한 설정
func DefaultCapabilities() []string {
	return []string{
		"CAP_CHOWN",            // 파일 UID, GID 변경 가능
		"CAP_DAC_OVERRIDE",     // 파일 접근 권한 검사 우회
		"CAP_FSETID",           // SetUID, SetGID 비트 해제 방지
		"CAP_FOWNER",           // 파일 소유자 검사 우회
		"CAP_MKNOD",            // 특수 파일 (장치) 생성 가능
		"CAP_NET_RAW",          // RAW 소켓 및 PACKET 소켓 생성
		"CAP_SETGID",           // GID 수정 가능
		"CAP_SETUID",           // UID 수정 가능
		"CAP_SETFCAP",          // 파일에 리눅스 권한(security.capability) 설정 가능
		"CAP_SETPCAP",          // 프로세스 capability 수정 가능
		"CAP_NET_BIND_SERVICE", // 특권 포트 (1024 이하)에 소켓 바인딩 가능
		"CAP_SYS_CHROOT",       // 프로세스의 루트 파일 시스템 경로 변경 가능
		"CAP_KILL",             // 시그널(SIGKILL, SIGTERM, SIGHUP 등) 전송 가능
		"CAP_AUDIT_WRITE",      // 커널 감사 로그에 기록 가능
	}
}
