package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func handleImagesCommand(r *Runtime, args []string) {
	imgCmd := flag.NewFlagSet("images", flag.ExitOnError)

	ns := imgCmd.String("ns", "default", "네임스페이스 지정 (기본 : default)")
	name := imgCmd.String("name", "", "이미지 이름, 태그, 레지스트리 부분 일치 필터 (예: nginx, docker )")
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

	imgCmd.Parse(args)

	handleImages(r, *ns, *name, *platform)

}

func handleImages(r *Runtime, ns string, imgName, imgPlatform string) {

	ctx := namespaces.WithNamespace(context.Background(), ns)
	imageList, err := r.client.ListImages(ctx)

	if err != nil {
		log.Fatalf("이미지 목록 조회 실패 : %v", err)
	}

	var targetMatcher platforms.MatchComparer
	var parsedPlatform ocispec.Platform

	if imgPlatform != "" {
		p, err := platforms.Parse(imgPlatform)

		if err != nil {
			log.Fatalf("잘못된 플랫폼 형식: %v", err)
		}
		parsedPlatform = p
		targetMatcher = platforms.Only(p) // 플랫폼 매칭
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "REPOSITORY\tIMAGE ID\tSIZE\tPLATFORM STATUS")

	hostPlatform := platforms.DefaultString()
	cs := r.client.ContentStore()

	for _, img := range imageList {

		fullName := img.Name()

		if imgName != "" {
			if !strings.Contains(strings.ToLower(fullName), strings.ToLower(imgName)) {
				continue
			}
		}
		shortDigest := img.Target().Digest.Hex()

		if len(shortDigest) > 12 {
			shortDigest = shortDigest[:12]
		}

		meta := img.Metadata()
		var sizeStr string
		var platformStatus string

		if targetMatcher != nil {
			targetSize, err := meta.Size(ctx, cs, targetMatcher)
			if err != nil || targetSize == 0 {
				continue
			}

			sizeStr = formatBytes(targetSize)
			if platforms.Format(parsedPlatform) == hostPlatform {
				platformStatus = fmt.Sprintf("[일치] (호스트 %s)", hostPlatform)
			} else {
				platformStatus = fmt.Sprintf("[불일치] (%s / 호스트 : %s)", imgPlatform, hostPlatform)
			}
		} else {

			size, err := img.Size(ctx)

			if err == nil {
				sizeStr = formatBytes(size)
				platformStatus = fmt.Sprintf("[일치] (호스트 %s)", hostPlatform)
			} else {

				sizeStr = "N/A"
				platformStatus = fmt.Sprintf("[불일치] 호스트(%s)와 다름", hostPlatform)

				children, childErr := images.Children(ctx, cs, img.Target())

				if childErr == nil {
					for _, child := range children {
						if child.Platform != nil {
							if _, infoErr := cs.Info(ctx, child.Digest); infoErr == nil {
								pMatcher := platforms.Only(*child.Platform)
								if s, sErr := meta.Size(ctx, cs, pMatcher); sErr == nil && s > 0 {
									sizeStr = formatBytes(s)
									platformStatus = fmt.Sprintf("[불일치] 다운로드: %s (호스트 : %s)", platforms.Format(*child.Platform), hostPlatform)
									break
								}
							}
						}
					}
				}
			}
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			fullName,
			shortDigest,
			sizeStr,
			platformStatus,
		)

	}
	tw.Flush()

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
