package cli

import (
	"context"
	"flag"
	"fmt"

	ctd "github.com/oyt2020/gocker/pkg/containerd"
)

func (d *Dispatcher) handlePull(ctx context.Context, args []string) error {
	pullCmd := flag.NewFlagSet("pull", flag.ContinueOnError)

	platform := pullCmd.String("platform", "", "이미지를 다운로드할 대상 플랫폼 지정 (예: linux/amd64, linux/arm64)")
	snapshotter := pullCmd.String("snapshotter", "", "이미지 레이어를 파일 시스템으로 풀어낼 snapshotter 지정 (예: overlayfs, native)")
	namespace := pullCmd.String("ns", "default", "네임스페이스 지정")

	pullCmd.Usage = func() {
		fmt.Println("Usage: gocker pull [OPTIONS] <image>")
		fmt.Println("\n이미지를 다운로드할 수 있습니다.")
		fmt.Println("\nOptions:")
		pullCmd.PrintDefaults()

		fmt.Println("\nExamples:")
		fmt.Println("  # 이미지 다운로드")
		fmt.Println("  gocker pull docker.io/library/busybox:latest")

		fmt.Println("\n  # 특정 플랫폼의 이미지 다운로드")
		fmt.Println("  gocker pull --platform=linux/amd64 docker.io/library/busybox:latest")

		fmt.Println("\n  # 사용할 snapshotter 지정")
		fmt.Println("  gocker pull --snapshotter=overlayfs docker.io/library/busybox:latest")

		fmt.Println("\n  # 플랫폼과 snapshotter를 함께 지정")
		fmt.Println("  gocker pull --platform=linux/amd64 --snapshotter=overlayfs docker.io/library/busybox:latest")
	}

	if err := pullCmd.Parse(args); err != nil {
		return err
	}

	remainArgs := pullCmd.Args()

	if len(remainArgs) < 1 {
		return fmt.Errorf("다운로드할 이미지를 지정해야 합니다. \n도움말 'gocker pull -h' 확인")
	}
	if len(remainArgs) > 1 {
		return fmt.Errorf("이미지는 동시에 하나만 다운로드할 수 있습니다.\n도움말 'gocker pull -h' 확인")
	}

	c, err := d.ctdSupplier()
	if err != nil {
		return err
	}
	defer func() {
		_ = c.Close()
	}()

	opts := ctd.PullOptions{
		Namespace:      *namespace,
		TargetPlatform: *platform,
		Snapshotter:    *snapshotter,
	}

	return c.Pull(
		ctx,
		remainArgs[0],
		opts,
	)
}
