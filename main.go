package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/oyt2020/gocker/pkg/cli"
	ctd "github.com/oyt2020/gocker/pkg/containerd"
	"github.com/oyt2020/gocker/pkg/runtime"
)

// containerd 기본 소켓 경로
const defaultContainerdSocket = "/run/containerd/containerd.sock"

func main() {
	// Ctrl+C(SIGINT) / SIGTERM을 받으면 ctx를 취소하여 진행 중인 명령이 정리하고 반환할 기회를 준다.
	// 취소 후에는 시그널 처리를 기본 동작으로 되돌리므로, 한 번 더 누르면 즉시 종료된다.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stopSignals)

	// 1. Phase 2 OCI / runc 기반 독립 런타임 초기화
	rt := runtime.NewContainerRuntime(nil, nil, nil, nil, nil)

	// 2. containerd 클라이언트 지연 생성 함수 (pull, images, exec, version 호출 시에만 온디맨드 연결)
	ctdSupplier := func() (*ctd.Client, error) {
		return ctd.NewClient(defaultContainerdSocket)
	}

	dispatcher := cli.NewDispatcher(rt, ctdSupplier)

	if err := dispatcher.Execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "gocker: %v\n", err)
		os.Exit(1)
	}
}
