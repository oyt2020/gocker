//go:build linux

package runc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
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
	// UnixListener.Close가 소켓 파일을 이미 지우므로(unlink) 파일이 없는 것은 에러가 아니다
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
				// 기대하지 않은 추가 fd는 누수되지 않도록 즉시 닫는다
				for _, extra := range fds[1:] {
					_ = syscall.Close(extra)
				}
				// runc가 보낸 master fd는 blocking 모드를 os.NewFile 전에 non-blocking으로 변경
				if err := syscall.SetNonblock(fds[0], true); err != nil {
					_ = syscall.Close(fds[0])
					return nil, fmt.Errorf("set pty master non-blocking: %w", err)
				}
				masterFile := os.NewFile(uintptr(fds[0]), "/dev/ptmx")
				return masterFile, nil
			}
		}
	}

	return nil, fmt.Errorf("no SCM_RIGHTS file descriptor received from runc (total control msgs: %d)", len(scms))
}

// consoleMasterTimeout runc create 이후 PTY master fd 수신을 기다리는 최대 시간
const consoleMasterTimeout = 5 * time.Second

// masterResult PTY Master FD 수신 결과
type masterResult struct {
	file *os.File
	err  error
}

// socketConsole : ConsoleSocket 위에 수신 고루틴과 회수 규칙을 얹은 Console 구현
type socketConsole struct {
	sock    *ConsoleSocket
	timeout time.Duration
	// ch : 수신 고루틴이 결과를 1개 넣고 닫는다. 닫힌 뒤의 수신은 즉시 반환되므로
	// "이미 꺼낸 뒤 다시 기다려 영구 블로킹"되는 경로가 없다.
	ch chan masterResult

	closeOnce sync.Once
	closeErr  error
}

// NewConsole : 콘솔 소켓을 만들고 master fd 수신을 백그라운드로 시작한다.
// runc는 create 도중 소켓에 연결해 fd를 보내므로, 수신은 반드시 runc create 호출 전에 시작되어 있어야 한다.
func (r *runcClient) NewConsole() (Console, error) {
	sock, err := NewConsoleSocket("")
	if err != nil {
		return nil, err
	}
	return newSocketConsole(sock, consoleMasterTimeout), nil
}

func newSocketConsole(sock *ConsoleSocket, timeout time.Duration) *socketConsole {
	c := &socketConsole{
		sock:    sock,
		timeout: timeout,
		ch:      make(chan masterResult, 1),
	}
	go func() {
		f, err := sock.ReceiveMaster()
		c.ch <- masterResult{file: f, err: err}
		close(c.ch)
	}()
	return c
}

func (c *socketConsole) Path() string {
	return c.sock.Path()
}

func (c *socketConsole) WaitMaster(ctx context.Context) (*os.File, error) {
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()

	select {
	case res, ok := <-c.ch:
		if !ok {
			return nil, fmt.Errorf("pty master already taken or console closed")
		}
		if res.err != nil {
			if res.file != nil {
				_ = res.file.Close()
			}
			return nil, res.err
		}
		return res.file, nil
	case <-ctx.Done():
		_ = c.Close()
		return nil, ctx.Err()
	case <-timer.C:
		_ = c.Close()
		return nil, fmt.Errorf("timeout waiting for pty master from runc (%s expired)", c.timeout)
	}
}

func (c *socketConsole) Close() error {
	c.closeOnce.Do(func() {
		// 리스너와 수락된 연결을 닫아 AcceptUnix / ReadMsgUnix에서 대기 중인 수신 고루틴을 깨운다
		c.closeErr = c.sock.Close()
		// 수신 고루틴이 끝날 때까지 기다리고, 아무도 가져가지 않은 master가 있으면 닫는다
		if res, ok := <-c.ch; ok && res.file != nil {
			_ = res.file.Close()
		}
	})
	return c.closeErr
}
