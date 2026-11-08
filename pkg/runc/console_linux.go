//go:build linux

package runc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// ConsoleSocket : 유닉스 도메인 소켓 관리 구조체, runc로부터 Master FD 수신 후 관리 함
type ConsoleSocket struct {
	path     string
	listener *net.UnixListener // 서버 측 리스너

	mu     sync.Mutex
	conn   *net.UnixConn // 수락된 연결 (Close 시 함께 닫아 ReadMsgUnix 대기 해제)
	closed bool
}

// NewConsoleSocket : 새로운 ConsoleSocket 인스턴스 생성 (파라미터 : 소켓 파일 경로)
func NewConsoleSocket(socketPath string) (*ConsoleSocket, error) {
	if socketPath == "" {
		tempDir := os.TempDir()
		socketPath = filepath.Join(tempDir, fmt.Sprintf("gocker-console-%d.sock", os.Getpid()))
	}

	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath) // net.Listen 반환 타입 : Listener (내부 구현체 : *net.UnixListener)
	if err != nil {
		return nil, fmt.Errorf("listen on console socket %s: %w", socketPath, err)
	}

	return &ConsoleSocket{
		path:     socketPath,
		listener: l.(*net.UnixListener),
	}, nil
}

// Path : ConsoleSocket 경로 반환
func (c *ConsoleSocket) Path() string {
	return c.path
}

// Close : ConsoleSocket 리스너 및 수락된 연결을 닫고 소켓 파일 삭제
func (c *ConsoleSocket) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.mu.Unlock()

	_ = c.listener.Close() // AcceptUnix 대기 해제
	if conn != nil {
		_ = conn.Close() // ReadMsgUnix 대기 해제
	}
	return os.Remove(c.path)
}

// ReceiveMaster : runc 연결 대기 / SCM_RIGHTS(FD 전달할 때 사용하는 제어 메시지) 프로토콜을 통해 Master FD 제어
func (c *ConsoleSocket) ReceiveMaster() (*os.File, error) {
	conn, err := c.listener.AcceptUnix() // 연결 수락
	if err != nil {
		return nil, fmt.Errorf("accept console connection: %w", err)
	}

	c.mu.Lock()
	if c.closed { // Accept와 Close 사이 경쟁 방지
		c.mu.Unlock()
		_ = conn.Close()
		return nil, fmt.Errorf("console socket closed")
	}
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		_ = conn.Close()
	}()

	// Buffer for payload data and out-of-band control messages (SCM_RIGHTS)
	buf := make([]byte, 1)                    // 소켓에서 수신한 원본 바이트
	oob := make([]byte, syscall.CmsgSpace(4)) // 4 bytes for 1 int fd

	_, oobn, _, _, err := conn.ReadMsgUnix(buf, oob) // oob에 Control Message 저장 . oobn은 OOB 버퍼에 수신된 데이터 바이트 수 저장
	if err != nil {
		return nil, fmt.Errorf("read msg from console socket: %w", err)
	}

	scms, err := syscall.ParseSocketControlMessage(oob[:oobn]) // 원본 바이트를 SocketControlMessage 구조체로 분리
	if err != nil {
		return nil, fmt.Errorf("parse socket control msg: %w", err)
	}

	// 제어 메시지 슬라이스를 순회하며 SOL_SOCKET / SCM_RIGHTS 타입의 FD 메시지 탐색
	for _, scm := range scms {
		if scm.Header.Level == syscall.SOL_SOCKET && scm.Header.Type == syscall.SCM_RIGHTS {
			fds, err := syscall.ParseUnixRights(&scm) // FD 파싱
			if err != nil {
				return nil, fmt.Errorf("parse unix rights: %w", err)
			}
			if len(fds) > 0 {
				masterFile := os.NewFile(uintptr(fds[0]), "/dev/ptmx")
				return masterFile, nil
			}
		}
	}

	return nil, fmt.Errorf("no SCM_RIGHTS file descriptor received from runc (total control msgs: %d)", len(scms))
}
