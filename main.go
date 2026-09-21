package main

import (
	"context"
	"fmt"
	"log"
	"os"
)

// containerd 소켓 경로
const defaultSocket = "/run/containerd/containerd.sock"

func main() {

	ctx := context.Background()

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
		if err := handlePullCommand(ctx, runtime, os.Args[2:]); err != nil {
			log.Printf("gocker: %v\n", err)
			os.Exit(1)
		}
	case "images":
		if err := handleImagesCommand(ctx, runtime, os.Args[2:]); err != nil {
			log.Printf("gocker: %v\n", err)
			os.Exit(1)
		}
	case "run":
		handleRunCommand(runtime, os.Args[2:])
	case "ps":
		handlePsCommand(runtime, os.Args[2:])
		//handlePs(runtime, "default", false)
	case "stop":
		handleStopCommand(runtime, os.Args[2:])
		//handleStop(runtime, "default", 10, []string{"f9401a0c54b7"})
	case "start":
		handleStartCommand(runtime, os.Args[2:])
		//handleStart(runtime, "default", false, []string{"my_nginx"})
	case "exec":
		result, err := handleExecCommand(
			ctx,
			runtime,
			os.Args[2:])
		if err != nil {
			log.Printf("gocker: %v\n", err)
			os.Exit(1)
		}

		if result.ExitCode != 0 {
			os.Exit(int(result.ExitCode))
		}
		//handleExec(runtime, "default", "my_nginx", false, false, []string{"ls", "-al", "/usr/share/nginx/html"})
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
