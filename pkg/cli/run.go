package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/oyt2020/gocker/pkg/image"
	"github.com/oyt2020/gocker/pkg/runtime"
	"golang.org/x/term"
)

func (d *Dispatcher) handleRun(ctx context.Context, args []string) error {
	runCmd := flag.NewFlagSet("run", flag.ContinueOnError)

	name := runCmd.String("name", "", "컨테이너 이름 지정")
	detach := runCmd.Bool("d", false, "백그라운드에서 실행")
	rm := runCmd.Bool("rm", false, "컨테이너 종료 시 자동 삭제")
	tty := runCmd.Bool("t", false, "가상 터미널(TTY) 할당")
	interactive := runCmd.Bool("i", false, "표준 입력(stdin) 유지")
	it := runCmd.Bool("it", false, "대화형 가상 터미널 할당 (-i -t)")
	rootfs := runCmd.String("rootfs", "", "RootFS 아카이브 파일 경로 (기본값: "+image.DefaultRootFS+")")
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

	rootfsPath, cmdArgs := image.ResolveArgs(*rootfs, runCmd.Args())

	opts := runtime.RunOptions{
		CreateOptions: runtime.CreateOptions{
			Name:          *name,
			RootfsTarPath: rootfsPath,
			Command:       cmdArgs,
			Terminal:      *tty,
			Hostname:      *hostname,
		},
		Detach: *detach,
		Remove: *rm,
	}

	// stdio 연결 정책은 CLI가 결정한다:
	//   - detach: 출력 대상을 지정하지 않음 → 런타임이 container.log로 기록 (gocker의 stdout을 물고 있지 않도록)
	//   - foreground: 컨테이너 출력을 현재 터미널로 연결
	if !*detach {
		opts.CreateOptions.Stdout = os.Stdout
		opts.CreateOptions.Stderr = os.Stderr
		if *tty {
			opts.Stdin = os.Stdin
		}
	}

	// 포그라운드 + TTY: 현재 터미널을 raw 모드로 전환 (키 입력을 컨테이너 PTY로 그대로 전달)
	// Run은 반환 전에 출력 복사를 끝내므로, 반환 직후의 복구 시점에는 컨테이너 출력이 더 이상 쓰이지 않는다.
	// 복구는 defer가 아니라 Run 직후 명시적으로 호출한다 (아래 os.Exit는 defer를 실행하지 않음).
	restore := func() {}
	if *tty && !*detach {
		if r, err := makeRawTerminal(os.Stdin); err == nil {
			restore = r
		} // 전환 실패 시 raw 모드 없이 진행 (기존 동작 유지)
	}

	result, err := d.rt.Run(ctx, opts)
	restore()
	if err != nil {
		return err
	}

	if *detach {
		fmt.Println(result.ID)
		return nil
	}

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}

// makeRawTerminal : f가 터미널이면 raw 모드로 전환하고 복구 함수를 반환한다 (터미널이 아니면 아무것도 하지 않음)
func makeRawTerminal(f *os.File) (restore func(), err error) {
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(fd, oldState) }, nil
}
