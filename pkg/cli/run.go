package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/oyt2020/gocker/pkg/runtime"
	"github.com/oyt2020/gocker/pkg/util"
)

func (d *Dispatcher) handleRun(ctx context.Context, args []string) error {
	runCmd := flag.NewFlagSet("run", flag.ContinueOnError)

	name := runCmd.String("name", "", "컨테이너 이름 지정")
	detach := runCmd.Bool("d", false, "백그라운드에서 실행")
	rm := runCmd.Bool("rm", false, "컨테이너 종료 시 자동 삭제")
	tty := runCmd.Bool("t", false, "가상 터미널(TTY) 할당")
	interactive := runCmd.Bool("i", false, "표준 입력(stdin) 유지")
	it := runCmd.Bool("it", false, "대화형 가상 터미널 할당 (-i -t)")
	rootfs := runCmd.String("rootfs", "", "RootFS 아카이브 파일 경로 (기본값: assets/alpine-minirootfs.tar.gz)")
	hostname := runCmd.String("hostname", "", "컨테이너 호스트명 지정")

	runCmd.Usage = func() {
		fmt.Println("Usage: gocker run [OPTIONS] [ROOTFS_OR_IMAGE] [COMMAND] [ARG...]")
		fmt.Println("\n새 컨테이너를 생성하고 시작합니다.")
		fmt.Println("\nOptions:")
		runCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 컨테이너 백그라운드 실행")
		fmt.Println("  gocker run -d --name my-box alpine")
		fmt.Println("\n  # 대화형 쉘 실행")
		fmt.Println("  gocker run -it alpine sh")
		fmt.Println("\n  # 일회성 명령어 실행 후 자동 삭제")
		fmt.Println("  gocker run --rm alpine echo 'Hello Gocker'")
		fmt.Println("\n  # 특정 RootFS 아카이브 지정 실행")
		fmt.Println("  gocker run --rootfs assets/alpine-minirootfs.tar.gz sh")
	}

	if err := runCmd.Parse(args); err != nil {
		return err
	}

	if *it {
		*tty = true
		*interactive = true
	}

	remainArgs := runCmd.Args()

	rootfsPath := *rootfs
	var cmdArgs []string

	if rootfsPath != "" {
		cmdArgs = remainArgs
	} else if len(remainArgs) > 0 {
		first := remainArgs[0]
		if strings.HasSuffix(first, ".tar") || strings.HasSuffix(first, ".tar.gz") || strings.HasSuffix(first, ".tgz") {
			rootfsPath = first
			cmdArgs = remainArgs[1:]
		} else if first == "alpine" || first == "alpine:latest" {
			rootfsPath = "assets/alpine-minirootfs.tar.gz"
			cmdArgs = remainArgs[1:]
		} else if _, err := os.Stat(first); err == nil {
			rootfsPath = first
			cmdArgs = remainArgs[1:]
		} else {
			rootfsPath = "assets/alpine-minirootfs.tar.gz"
			cmdArgs = remainArgs
		}
	} else {
		rootfsPath = "assets/alpine-minirootfs.tar.gz"
	}

	if len(cmdArgs) == 0 {
		cmdArgs = []string{"/bin/sh"}
	}

	containerID, err := util.GenerateID()
	if err != nil {
		return fmt.Errorf("컨테이너 ID 생성 실패: %w", err)
	}

	opts := runtime.RunOptions{
		CreateOptions: runtime.CreateOptions{
			ID:            containerID,
			Name:          *name,
			RootfsTarPath: rootfsPath,
			Command:       cmdArgs,
			Terminal:      *tty,
			Hostname:      *hostname,
			Stdout:        os.Stdout,
			Stderr:        os.Stderr,
		},
		Detach: *detach,
		Remove: *rm,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	exitCode, err := d.rt.Run(ctx, opts)
	if err != nil {
		return err
	}

	if *detach {
		fmt.Println(containerID)
		return nil
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
