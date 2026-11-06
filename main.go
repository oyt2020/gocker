package main

import (
	"context"
	"fmt"
	"os"

	"github.com/oyt2020/gocker/pkg/cli"
	ctd "github.com/oyt2020/gocker/pkg/containerd"
	"github.com/oyt2020/gocker/pkg/runtime"
)

// containerd 기본 소켓 경로
const defaultContainerdSocket = "/run/containerd/containerd.sock"

func main() {
	ctx := context.Background()

	// 1. Phase 2 OCI / runc 기반 독립 런타임 초기화
	rt := runtime.NewContainerRuntime(nil, nil, nil)

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
