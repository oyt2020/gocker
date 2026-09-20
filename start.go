package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

func handleStartCommand(r *Runtime, args []string) {
	startCmd := flag.NewFlagSet("start", flag.ExitOnError)

	ns := startCmd.String("ns", "default", "네임스페이스 지정")
	attach := startCmd.Bool("attach", false, "포그라운드 실행")

	startCmd.Usage = func() {
		fmt.Println("Usage: gocker start [OPTIONS] CONTAINER [CONTAINER...]")
		fmt.Println("\n하나 이상의 중지된 컨테이너를 다시 시작합니다.")
		fmt.Println("\nOptions:")
		startCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 백그라운드로 다시 시작 (기본)")
		fmt.Println("  gocker start my-nginx")
		fmt.Println("\n  # 여러 컨테이너 동시 시작")
		fmt.Println("  gocker start my-nginx c8a19d3f10be")
		fmt.Println("\n  # 포그라운드로 실행 화면 연결")
		fmt.Println("  gocker start -attach my-nginx")
	}

	startCmd.Parse(args)

	targets := startCmd.Args()
	if len(targets) < 1 {
		startCmd.Usage()
		log.Fatal("중지할 컨테이너 이름을 지정해야 합니다.")
	}

	handleStart(r, *ns, *attach, targets)
}

func handleStart(r *Runtime, ns string, attach bool, target []string) {

	ctx := namespaces.WithNamespace(context.Background(), ns)

	for _, target := range target {

		container, err := r.client.LoadContainer(ctx, target)

		if err != nil {
			log.Fatalf("첫 번째 에러 (컨테이너 찾기 실패) %v", err)
		}

		task, err := container.Task(ctx, nil)
		if err == nil {
			status, sErr := task.Status(ctx)
			if sErr == nil && status.Status == client.Running {
				log.Fatalf("두 번째 에러 (이미 실행중인 Task) %v", err)
			}

			// 잔여 Task 정리
			_, _ = task.Delete(ctx)
		}

		isTTY := false

		spec, specErr := container.Spec(ctx)

		if specErr == nil && spec.Process != nil && spec.Process.Terminal {
			isTTY = true
		}

		var ioCreator cio.Creator

		if attach {
			cioOpts := []cio.Opt{cio.WithStdio}

			if isTTY {
				cioOpts = append(cioOpts, cio.WithTerminal)
			}

			ioCreator = cio.NewCreator(cioOpts...)
		} else {
			ioCreator = cio.NullIO
		}

		newTask, err := container.NewTask(ctx, ioCreator)
		if err != nil {
			log.Fatalf("세 번째 에러 (Task 생성 실패) %v", err)
		}

		if err := newTask.Start(ctx); err != nil {
			_, _ = newTask.Delete(ctx)
			log.Fatalf("네 번째 에러 (Task 시작 실패) %v", err)
		}

		if !attach {
			fmt.Println(container.ID())
			continue
		}

		statusC, err := newTask.Wait(ctx)
		if err != nil {
			log.Fatalf("다섯 번째 에러 (Task 대기 실패) %v", err)
		}

		status := <-statusC

		code, _, _ := status.Result()

		_, _ = newTask.Delete(ctx)

		if code != 0 {
			os.Exit(int(code))
		}
	}
}
