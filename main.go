package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/containerd/containerd/v2/client"         // containerd client 패키지
	"github.com/containerd/containerd/v2/pkg/namespaces" // 네임스페이스 패키지
	"github.com/containerd/platforms"
)

// containerd 소켓 경로
const defaultSocket = "/run/containerd/containerd.sock"

type Runtime struct {
	client *client.Client
}

func NewRuntime(socket string) (*Runtime, error) {
	c, err := client.New(socket)
	if err != nil {
		return nil, fmt.Errorf("connect containerd: %w", err) // 래핑된 에러 객체로 반환
	}

	return &Runtime{
		client: c,
	}, nil
}

func (r *Runtime) Close() error {
	return r.client.Close()
}

func main() {

	// main 함수에서 단 한 개의 클라이언트 생성
	runtime, err := NewRuntime(defaultSocket)
	if err != nil {
		log.Fatal(err) // 에러 출력하고 바로 프로그램 종료
	}
	defer runtime.Close()

	if len(os.Args) < 2 {
		fmt.Println("Usage: gocker <command>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "version":
		handleVersion(runtime)
	case "pull":
		imgRef, platform, snapshotter, err := handlePullCommand(os.Args[2:])
		if err != nil {
			log.Fatal(err)
		}
		handlePull(runtime, imgRef, platform, snapshotter, "default")
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// Containerd 버전 출력 함수
func handleVersion(r *Runtime) {
	ctx := context.Background()

	ver, err := r.client.Version(ctx)
	if err != nil {
		log.Fatalf("failed to get containerd version: %v", err)
	}

	fmt.Printf("containerd version: %s\n", ver.Version)
	fmt.Printf("containerd Revision: %s\n", ver.Revision)
}

func handlePullCommand(args []string) (imgRef string, platform2 string, snapshotter2 string, err error) {
	pullCmd := flag.NewFlagSet("pull", flag.ExitOnError)

	platform := pullCmd.String("platform", "", "이미지를 다운로드할 대상 플랫폼 지정 (예: linux/amd64, linux/arm64)")
	snapshotter := pullCmd.String("snapshotter", "", "이미지 레이어를 파일 시스템으로 풀어낼 snapshotter 지정 (예: overlayfs, native)")

	// -h 플래그 출력
	pullCmd.Usage = func() {
		fmt.Println("Usage: gocker pull [OPTIONS] <image>")
		fmt.Println("\n이미지를 다운로드할 수 있습니다.")
		fmt.Println("\nOptions")
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

	pullCmd.Parse(args)

	// 인자 파싱
	remainArgs := pullCmd.Args()

	// 플래그 제외 인자 개수가 1개 미만이면 에러 반환
	if len(remainArgs) < 1 {
		return "", "", "", errors.New("다운로드할 이미지 지정해야 합니다. \n도움말 'gocker pull -h' 확인")
	}

	return remainArgs[0], *platform, *snapshotter, nil

}

func handlePull(r *Runtime, ref string, targetPlatform string, snapshotter string, ns string) {

	ctx := context.Background()

	// 네임스페이스 설정
	if ns == "" {
		ctx = namespaces.WithNamespace(ctx, "default")
	} else {
		ctx = namespaces.WithNamespace(ctx, ns)
	}

	// 이미지 다운로드 받을 때 추가할 옵션 모음
	// 기본 포함 : 언팩 (압축 해제) -> 이미지를 실행할 계획이면 필수
	// 기본 포함2 : 메타데이터 -> 다른 플랫폼의 메타데이터도 다운로드
	pullOpts := []client.RemoteOpt{
		client.WithPullUnpack,
		client.WithAllMetadata(),
	}

	// 추가 옵션 1
	// 이미지 환경 (linux/amd64, linux/arm64 등)
	// 미지정 시 현재 컴퓨터의 기본 플랫폼 사용
	if targetPlatform != "" {
		p, err := platforms.Parse(targetPlatform)
		if err != nil {
			log.Fatalf("잘못된 플랫폼 형식: %v", err)
		}
		pullOpts = append(pullOpts, client.WithPlatformMatcher(platforms.Only(p)))
	} else {
		pullOpts = append(pullOpts, client.WithPlatformMatcher(platforms.Default()))
	}

	// 추가 옵션 2
	// 이미지 파일 시스템 설정 (native, btrfs,stargz 등)
	// 기본값은 overlayfs
	if snapshotter != "" {
		pullOpts = append(pullOpts, client.WithPullSnapshotter(snapshotter))
	}
	_, err := r.client.Pull(ctx, ref, pullOpts...)

	// 에러
	if err != nil {
		log.Fatalf("다운로드 실패: %v", err)
	}
}
