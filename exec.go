package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"golang.org/x/term"
)

func handleExecCommand(r *Runtime, args []string) {
	execCmd := flag.NewFlagSet("exec", flag.ExitOnError)

	ns := execCmd.String("ns", "default", "네트워크 지정")
	tty := execCmd.Bool("t", false, "가상 터미널 할당")
	interactive := execCmd.Bool("i", false, "표준 입력 유지")
	detach := execCmd.Bool("d", false, "백그라운드 실행")

	it := execCmd.Bool("it", false, "대화형 가상 터미널 할당")

	execCmd.Usage = func() {
		fmt.Println("Usage: gocker exec [OPTIONS] CONTAINER COMMAND [ARG...]")
		fmt.Println("\n실행 중인 컨테이너 내부에서 새로운 명령어를 실행합니다.")
		fmt.Println("\nOptions:")
		execCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 실행 중인 컨테이너의 쉘에 접속")
		fmt.Println("  gocker exec -it my-nginx sh")
		fmt.Println("\n  # 컨테이너 내부 파일 목록 확인")
		fmt.Println("  gocker exec my-nginx ls -la /etc/nginx")
	}

	execCmd.Parse(args)

	if *it {
		*tty = true
		*interactive = true
	}

	remainArgs := execCmd.Args()

	if len(remainArgs) < 2 {
		execCmd.Usage()
		log.Fatal("에러: 대상 컨테이너와 실행할 명령어를 모두 입력해야 합니다.")
	}

	targetContainer := remainArgs[0]
	cmdArgs := remainArgs[1:]

	handleExec(r, *ns, targetContainer, *tty, *detach, cmdArgs)

}

func handleExec(r *Runtime, ns, target string, tty, detach bool, cmdArgs []string) {

	ctx := namespaces.WithNamespace(context.Background(), ns)

	container, err := r.client.LoadContainer(ctx, target)

	if err != nil {
		log.Fatalf("첫 번째 에러 (컨테이너 찾기 실패) %v", err)
	}

	task, err := container.Task(ctx, nil)

	if err != nil {
		log.Fatalf("두 번째 에러 (Task 찾기 실패) %v", err)
	}

	status, err := task.Status(ctx)

	if err != nil || status.Status != client.Running {
		log.Fatalf("세 번째 에러 (Task 상태가 실행중이 아님) %v", err)
	}

	spec, err := container.Spec(ctx)

	if err != nil {
		log.Fatalf("네 번째 에러 (Spec 조회 실패) %v", err)
	}

	pspec := *spec.Process
	pspec.Args = cmdArgs
	pspec.Terminal = tty

	var ioCreator cio.Creator
	if detach {
		ioCreator = cio.NullIO
	} else {
		cioOpts := []cio.Opt{cio.WithStdio}
		if tty {
			cioOpts = append(cioOpts, cio.WithTerminal)
		}
		ioCreator = cio.NewCreator(cioOpts...)
	}

	if tty && !detach {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}

	execID := fmt.Sprintf("exec-%s", generateRandomID())
	process, err := task.Exec(ctx, execID, &pspec, ioCreator)
	if err != nil {
		log.Fatalf("다섯 번째 에러 (Exec 생성 실패) %v", err)
	}

	defer process.Delete(ctx)

	if err := process.Start(ctx); err != nil {
		log.Fatalf("여섯 번째 에러 (Exec 시작 실패) %v", err)
	}

	if detach {
		return
	}

	statusC, err := process.Wait(ctx)
	if err != nil {
		log.Fatalf("일곱 번째 에러 (Exec 대기 실패) %v", err)
	}

	exitStatus := <-statusC
	code, _, _ := exitStatus.Result()

	if code != 0 {
		os.Exit(int(code))
	}

}

func generateRandomID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
