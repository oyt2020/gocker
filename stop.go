package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

func handleStopCommand(r *Runtime, args []string) {
	stopCmd := flag.NewFlagSet("stop", flag.ExitOnError)

	ns := stopCmd.String("ns", "default", "네임스페이스 지정")
	timeoutSec := stopCmd.Int("t", 10, "종료 대기 타임아웃 시간 지정 (단위 초, 기본 값 10초)")

	stopCmd.Usage = func() {
		fmt.Println("Usage: gocker stop [OPTIONS] CONTAINER")
		fmt.Println("\n 하나 이상의 실행 중인 컨테이너를 중지합니다.")
		fmt.Println("\nOptions")
		stopCmd.PrintDefaults()

		fmt.Println("\nExamples")
		fmt.Println(" # 단일 컨테이너 중지")
		fmt.Println(" gocker stop my_nginx")

		fmt.Println("\n # 여러 컨테이너 중지")
		fmt.Println(" gocker stop my_nginx my_busybox")

		fmt.Println("\n # 5초 대기 후 강제 종료")
		fmt.Println(" gocker stop -t=5 my_nginx")
	}

	stopCmd.Parse(args)

	targets := stopCmd.Args()
	if len(targets) < 1 {
		stopCmd.Usage()
		log.Fatal("중지할 컨테이너 이름을 지정해야 합니다.")
	}

	handleStop(r, *ns, *timeoutSec, targets)
}

func handleStop(r *Runtime, ns string, timeoutSec int, targets []string) {

	ctx := namespaces.WithNamespace(context.Background(), ns)

	for _, target := range targets {

		container, err := r.client.LoadContainer(ctx, target)

		if err != nil {
			log.Fatalf("첫 번째 에러 (컨테이너 찾기 실패) %v", err)
		}

		task, err := container.Task(ctx, nil)
		if err != nil {
			log.Fatalf("두 번째 에러 (Task 찾기 실패) %v", err)
		}

		status, err := task.Status(ctx)
		if err == nil && status.Status != client.Running {
			_, _ = task.Delete(ctx)
			log.Fatalf("세 번째 에러 (이미 종료된 컨테이너) %v", err)
		}

		exitStatusC, err := task.Wait(ctx)
		if err != nil {
			log.Fatalf("네 번째 에러 (컨테이너 대기 채널 생성 실패) %v", err)
		}

		if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
			log.Fatalf("다섯 번째 에러 (SIGTERM 신호 전송 실패) %v", err)
		}

		select {
		case <-exitStatusC:
		case <-time.After(time.Duration(timeoutSec) * time.Second):
			_ = task.Kill(ctx, syscall.SIGKILL)
			<-exitStatusC
		}

		_, _ = task.Delete(ctx)

		fmt.Println(container.ID())

	}

}
