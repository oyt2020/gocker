package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	ctd "github.com/oyt2020/gocker/pkg/containerd"
)

func (d *Dispatcher) handleExec(ctx context.Context, args []string) error {
	execCmd := flag.NewFlagSet("exec", flag.ContinueOnError)

	ns := execCmd.String("ns", "default", "네임스페이스 지정")
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
		return err
	}

	if *it {
		*tty = true
		*interactive = true
	}

	remainArgs := execCmd.Args()
	if len(remainArgs) < 2 {
		execCmd.Usage()
		return fmt.Errorf("대상 컨테이너와 실행할 명령어를 모두 입력해야 합니다")
	}

	c, err := d.ctdSupplier()
	if err != nil {
		return err
	}

	opts := ctd.ExecOptions{
		Namespace:   *ns,
		TTY:         *tty,
		Interactive: *interactive,
		Detach:      *detach,
	}

	result, err := c.Exec(
		ctx,
		remainArgs[0],
		remainArgs[1:],
		opts,
	)
	_ = c.Close()
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		os.Exit(int(result.ExitCode))
	}

	return nil
}
