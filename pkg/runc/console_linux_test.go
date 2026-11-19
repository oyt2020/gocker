//go:build linux

package runc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func newTestConsole(t *testing.T, timeout time.Duration) *socketConsole {
	t.Helper()
	sock, err := NewConsoleSocket(filepath.Join(t.TempDir(), "console.sock"))
	if err != nil {
		t.Fatalf("new console socket: %v", err)
	}
	c := newSocketConsole(sock, timeout)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// sendFD : runc처럼 콘솔 소켓에 연결해 SCM_RIGHTS로 fd 하나를 보낸다
func sendFD(t *testing.T, sockPath string, f *os.File) {
	t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		t.Fatalf("dial console socket: %v", err)
	}
	defer conn.Close()
	if _, _, err := conn.WriteMsgUnix([]byte{0}, syscall.UnixRights(int(f.Fd())), nil); err != nil {
		t.Fatalf("send fd: %v", err)
	}
}

func TestConsole_WaitMasterReceivesFD(t *testing.T) {
	c := newTestConsole(t, 2*time.Second)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer w.Close()
	sendFD(t, c.Path(), r)
	_ = r.Close() // 보낸 쪽 사본은 닫는다 (받은 쪽이 별도 fd를 가진다)

	master, err := c.WaitMaster(context.Background())
	if err != nil {
		t.Fatalf("wait master: %v", err)
	}
	defer master.Close()

	// Close는 이미 넘겨준 master를 건드리지 않아야 한다
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := w.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 2)
	if _, err := master.Read(buf); err != nil || string(buf) != "hi" {
		t.Fatalf("read from received master: %q, %v", buf, err)
	}

	// 받은 fd는 non-blocking(pollable)이어야 read deadline으로 대기 중인 Read를 깨울 수 있다
	if err := master.SetReadDeadline(time.Now()); err != nil {
		t.Fatalf("received master must support read deadline: %v", err)
	}

	// 두 번째 WaitMaster는 블로킹되지 않고 에러를 반환해야 한다
	if _, err := c.WaitMaster(context.Background()); err == nil {
		t.Fatalf("expected error on second WaitMaster")
	}
}

func TestConsole_CloseBeforeConnectDoesNotBlock(t *testing.T) {
	c := newTestConsole(t, 2*time.Second)

	done := make(chan struct{})
	go func() {
		_ = c.Close()
		_ = c.Close() // 멱등
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Close blocked")
	}

	if _, err := c.WaitMaster(context.Background()); err == nil {
		t.Fatalf("expected error from WaitMaster after Close")
	}
	if _, err := os.Stat(c.Path()); !os.IsNotExist(err) {
		t.Fatalf("socket file should be removed, stat err=%v", err)
	}
}

func TestConsole_CloseReclaimsUntakenMaster(t *testing.T) {
	c := newTestConsole(t, 2*time.Second)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer w.Close()
	sendFD(t, c.Path(), r)
	_ = r.Close()

	// master를 꺼내지 않고 닫는다: 도착한 fd가 회수되어 읽기 쪽이 모두 닫혀야 한다
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatalf("expected EPIPE: untaken master fd was leaked")
	}
}

func TestConsole_WaitMasterTimeoutAndCancel(t *testing.T) {
	c := newTestConsole(t, 50*time.Millisecond)
	if _, err := c.WaitMaster(context.Background()); err == nil {
		t.Fatalf("expected timeout error")
	}

	c2 := newTestConsole(t, 2*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c2.WaitMaster(ctx); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
