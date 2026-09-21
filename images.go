package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

func handleImagesCommand(ctx context.Context, r *Runtime, args []string) error {
	imgCmd := flag.NewFlagSet("images", flag.ContinueOnError)

	namespace := imgCmd.String("ns", "default", "네임스페이스 지정 (기본 : default)")
	imgName := imgCmd.String("name", "", "이미지 이름, 태그, 레지스트리 부분 일치 필터 (예: nginx, docker )")
	platform := imgCmd.String("platform", "", "이미지 플랫폼 지정 (예: linux/amd64, linux/arm64)")

	imgCmd.Usage = func() {
		fmt.Println("Usage: images [options]")
		fmt.Println("\n로컬에 있는 이미지 목록을 출력합니다.")
		fmt.Println("\nOptions")
		imgCmd.PrintDefaults()

		fmt.Println("\nExamples")
		fmt.Println(" # 로컬 이미지 전체 리스트")
		fmt.Println(" gocker images")

		fmt.Println("\n # 특정 이름을 포함하는 이미지 리스트")
		fmt.Println(" gocker images --name=<포함할 이름>")

		fmt.Println("\n # 특정 플랫폼 이미지 리스트")
		fmt.Println(" gocker images --platform=linux/amd64")
	}

	if err := imgCmd.Parse(args); err != nil {
		return err
	}

	remainArgs := imgCmd.Args()

	if len(remainArgs) != 0 {
		return fmt.Errorf("gocker images 명령은 추가 인자를 받지 않습니다. \n 도움말 'gocker images -h' 확인")
	}
	opts := ImagesOptions{
		Namespace:   *namespace,
		ImgName:     *imgName,
		ImgPlatform: *platform,
	}

	imgResult, err := r.Images(ctx, opts)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "REPOSITORY\tIMAGE ID\tSIZE\tPLATFORM STATUS"); err != nil {
		return err
	}

	for _, img := range imgResult {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			img.Repository,
			img.ImageID,
			img.Size,
			img.PlatformStatus,
		); err != nil {
			return err
		}
	}

	if err := tw.Flush(); err != nil {
		return err
	}

	return nil

}

func formatBytes(bytes int64) string {
	const uint = 1024

	if bytes < uint {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(uint), 0

	for n := bytes / uint; n >= uint; n /= uint {
		div *= uint
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
