package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/containerd/containerd/v2/client" // containerd client 패키지
)

// containerd 소켓 경로
const defaultSocket = "/run/containerd/containerd.sock"

type Runtime struct {
	client *client.Client
}

func NewRuntime(socket string) (*Runtime, error) {
	c, err := client.New(socket)
	if err != nil {
		return nil, fmt.Errorf("connect containerd: %w", err) // 래핑된 에러 객체로 반환
	}

	return &Runtime{
		client: c,
	}, nil
}

func (r *Runtime) Close() error {
	return r.client.Close()
}

func main() {

	// main 함수에서 단 한 개의 클라이언트 생성
	runtime, err := NewRuntime(defaultSocket)
	if err != nil {
		log.Fatal(err) // 에러 출력하고 바로 프로그램 종료
	}
	defer runtime.Close()

	if len(os.Args) < 2 {
		fmt.Println("Usage: gocker <command>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "version":
		handleVersion(runtime)
	case "pull":
		imgRef, platform, snapshotter, err := handlePullCommand(os.Args[2:])
		if err != nil {
			log.Fatal(err)
		}
		handlePull(runtime, imgRef, platform, snapshotter, "default")
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// Containerd 버전 출력 함수
func handleVersion(r *Runtime) {
	ctx := context.Background()

	ver, err := r.client.Version(ctx)
	if err != nil {
		log.Fatalf("failed to get containerd version: %v", err)
	}

	fmt.Printf("containerd version: %s\n", ver.Version)
	fmt.Printf("containerd Revision: %s\n", ver.Revision)
}
