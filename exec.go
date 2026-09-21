package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
)

func handleExecCommand(ctx context.Context, r *Runtime, args []string) (ExecResult, error) {
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

	if err := execCmd.Parse(args); err != nil {
		return ExecResult{}, err
	}

	if *it {
		*tty = true
		*interactive = true
	}

	remainArgs := execCmd.Args()

	if len(remainArgs) < 2 {
		execCmd.Usage()
		return ExecResult{},
			fmt.Errorf("대상 컨테이너와 실행할 명령어를 모두 입력해야 합니다")
	}

	opts := ExecOptions{
		Namespace:   *ns,
		TTY:         *tty,
		Interactive: *interactive,
		Detach:      *detach,
	}

	return r.Exec(
		ctx,
		remainArgs[0],
		remainArgs[1:],
		opts,
	)
}

func generateRandomID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
