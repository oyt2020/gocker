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
	"github.com/containerd/containerd/v2/pkg/oci"
)

func handleRunCommand(ctx context.Context, r *Runtime, args []string) error {

	runCmd := flag.NewFlagSet("run", flag.ContinueOnError)

	ns := runCmd.String("ns", "default", "네임스페이스 지정")
	name := runCmd.String("name", "", "컨테이너 이름 지정")
	detach := runCmd.Bool("d", false, "백그라운드에서 실행")
	rm := runCmd.Bool("rm", false, "컨테이너 종료 시 자동 삭제")
	tty := runCmd.Bool("t", false, "가상 터미널 할당")

	runCmd.Usage = func() {
		fmt.Println("Usage: gocker run [OPTIONS] IMAGE [COMMAND] [ARG...]")
		fmt.Println("\n컨테이너를 생성 및 시작합니다.")
		fmt.Println(" \n 주의")
		fmt.Println(" 동시에 하나의 컨테이너만 생성 및 시작할 수 있습니다.")
		fmt.Println("\nOptions:")
		runCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 컨테이너 시작 (기본)")
		fmt.Println("  gocker run -d --name my-nginx docker.io/library/nginx:latest")
		fmt.Println("  # 포그라운드 일회성 명령어 실행")
		fmt.Println("  gocker run --rm docker.io/library/busybox:latest echo Hello Gocker")
		fmt.Println("  # 가상 터미널 할당")
		fmt.Println("  gocker run -t docker.io/library/nginx:latest sh")
	}

	if err := runCmd.Parse(args); err != nil {
		return err
	}

	remainArgs := runCmd.Args()

	if len(remainArgs) < 1 {
		return fmt.Errorf("실행할 이미지 이름을 지정해야 합니다. \n 도움말 'gocker run -h' 확인")
	}

	imgRef := remainArgs[0]
	cmdArgs := remainArgs[1:]

	opts := RunOptions{
		Namespace:   *ns,
		ContainerID: *name,
		ImageRef:    imgRef,
		Detach:      *detach,
		Rm:          *rm,
		TTY:         *tty,
		Command:     cmdArgs,
	}

	result, err := r.Run(ctx, opts)

	if err != nil {
		return err
	}

	fmt.Println(result.ContainerID)
	if !*detach {
		fmt.Printf("종료 코드 %d \n", result.ExitCode)
	}

	return nil

}

// 컨테이너 시작
// Container -> Task
func handleRun(r *Runtime, ns, containerId, imageRef string, detach, rm, tty bool, cmdArgs []string) {

	ctx := namespaces.WithNamespace(context.Background(), ns)

	img, err := r.client.GetImage(ctx, imageRef)
	if err != nil {
		log.Fatalf("첫 번째 에러(이미지): %v", err)
	}

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
	}

	// 사용자가 넘긴 명령어 적용
	if len(cmdArgs) > 0 {
		specOpts = append(specOpts, oci.WithProcessArgs(cmdArgs...))
	}

	// 이름 미지정 시 자동 생성
	if containerId == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		containerId = hex.EncodeToString(b)
	}

	if tty {
		specOpts = append(specOpts, oci.WithTTY)
	}

	container, err := r.client.NewContainer(
		ctx,
		containerId,
		client.WithImage(img),
		client.WithNewSnapshot(containerId, img),
		client.WithNewSpec(specOpts...),
	)

	if err != nil {
		log.Fatalf("두 번째 에러 (컨테이너 메타데이터): %v", err)
	}

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

	task, err := container.NewTask(ctx, ioCreator)

	if err != nil {
		log.Fatalf("세 번째 에러 (Task): %v", err)
	}

	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx)

		_ = container.Delete(ctx, client.WithSnapshotCleanup)
		log.Fatalf("네 번째 에러 (Task 실행 실패) %v", err)
	}

	if detach {
		fmt.Println(containerId)
		return
	}

	statusC, err := task.Wait(ctx)
	if err != nil {
		log.Fatalf("다섯 번째 에러 (Task 대기 실패) %v", err)
	}

	status := <-statusC
	code, _, err := status.Result()
	if err != nil {
		log.Fatalf("여섯 번째 에러 (Task 종료 수신 실패) %v", err)
	}

	_, _ = task.Delete(ctx)

	if rm {
		_ = container.Delete(ctx, client.WithSnapshotCleanup)
	}

	if code != 0 {
		os.Exit(int(code))
	}

}
