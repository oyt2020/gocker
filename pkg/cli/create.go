package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/oyt2020/gocker/pkg/image"
	"github.com/oyt2020/gocker/pkg/runtime"
)

func (d *Dispatcher) handleCreate(ctx context.Context, args []string) error {
	createCmd := flag.NewFlagSet("create", flag.ContinueOnError)

	name := createCmd.String("name", "", "컨테이너 이름 지정")
	tty := createCmd.Bool("t", false, "가상 터미널(TTY) 할당")
	rootfs := createCmd.String("rootfs", "", "RootFS 아카이브 파일 경로 (기본값: "+image.DefaultRootFS+")")
	hostname := createCmd.String("hostname", "", "컨테이너 호스트명 지정")

	createCmd.Usage = func() {
		fmt.Println("Usage: gocker create [OPTIONS] [ROOTFS_OR_IMAGE] [COMMAND] [ARG...]")
		fmt.Println("\nRootFS 아카이브로부터 새 컨테이너를 생성합니다 (실행은 하지 않음).")
		fmt.Println("\nOptions:")
		createCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 기본 alpine 이미지로 컨테이너 생성")
		fmt.Println("  gocker create --name my-box alpine")
		fmt.Println("\n  # 가상 터미널 할당 옵션으로 생성")
		fmt.Println("  gocker create -t --name test-box alpine sh")
		fmt.Println("\n  # 특정 RootFS 아카이브 지정 생성")
		fmt.Println("  gocker create --rootfs assets/alpine-minirootfs.tar.gz sh")
	}

	if err := createCmd.Parse(args); err != nil {
		return err
	}

	rootfsPath, cmdArgs := image.ResolveArgs(*rootfs, createCmd.Args())

	opts := runtime.CreateOptions{
		Name:          *name,
		RootfsTarPath: rootfsPath,
		Command:       cmdArgs,
		Terminal:      *tty,
		Hostname:      *hostname,
	}

	meta, err := d.rt.Create(ctx, opts)
	if err != nil {
		return fmt.Errorf("컨테이너 생성 실패: %w", err)
	}

	fmt.Println(meta.ID)
	return nil
}
