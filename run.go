package main

import (
	"context"
	"log"
	"os"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
)

// 컨테이너 시작
// Container -> Task
func handleRun(r *Runtime, ns, containerId, imageRef string, cmdArgs []string) {

	ctx := context.Background()

	if ns == "" {
		ctx = namespaces.WithNamespace(ctx, "default")
	} else {
		ctx = namespaces.WithNamespace(ctx, ns)
	}

	img, err := r.client.GetImage(ctx, imageRef)
	if err != nil {
		log.Fatalf("첫 번째 에러(이미지): %v", err)
	}

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
	}

	if len(cmdArgs) > 0 {
		specOpts = append(specOpts, oci.WithProcessArgs(cmdArgs...))
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

	ioCreator = cio.NewCreator(cio.WithStdio)

	task, err := container.NewTask(ctx, ioCreator)

	if err != nil {
		log.Fatalf("세 번째 에러 (Task): %v", err)
	}

	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx)

		_ = container.Delete(ctx, client.WithSnapshotCleanup)
		log.Fatalf("네 번째 에러 (Task 실행 실패) %v", err)
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

	if code != 0 {
		os.Exit(int(code))
	}

}
