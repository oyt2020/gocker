package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/platforms"
	"golang.org/x/term"
)

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

type ExecOptions struct {
	Namespace   string
	TTY         bool
	Interactive bool
	Detach      bool
}

type ExecResult struct {
	ExitCode uint32
}

func (r *Runtime) Exec(
	ctx context.Context,
	target string,
	cmdArgs []string,
	opts ExecOptions,
) (ExecResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)

	container, err := r.client.LoadContainer(ctx, target)

	if err != nil {
		return ExecResult{}, fmt.Errorf("컨테이너 찾기 실패 %q: %w", target, err)
	}

	task, err := container.Task(ctx, nil)

	if err != nil {
		return ExecResult{}, fmt.Errorf("task 찾기 실패 %q: %w", target, err)
	}

	status, err := task.Status(ctx)
	if err != nil {
		return ExecResult{}, fmt.Errorf("task 상태 확인 실패 %q: %w", target, err)
	}

	if status.Status != client.Running {
		return ExecResult{}, fmt.Errorf("task 상태가 실행중이 아님 %q : %s", target, status.Status)
	}

	spec, err := container.Spec(ctx)

	if err != nil {
		return ExecResult{}, fmt.Errorf("spec 조회 실패 %q: %w", target, err)
	}

	pspec := *spec.Process
	pspec.Args = cmdArgs
	pspec.Terminal = opts.TTY

	var ioCreator cio.Creator
	if opts.Detach {
		ioCreator = cio.NullIO
	} else {
		var in io.Reader

		if opts.Interactive {
			in = os.Stdin
		}

		cioOpts := []cio.Opt{
			cio.WithStreams(in, os.Stdout, os.Stderr),
		}
		if opts.TTY {
			cioOpts = append(cioOpts, cio.WithTerminal)
		}
		ioCreator = cio.NewCreator(cioOpts...)
	}

	if opts.TTY && !opts.Detach {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}

	execID := fmt.Sprintf("exec-%s", generateRandomID())
	process, err := task.Exec(ctx, execID, &pspec, ioCreator)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec 생성 실패 %q: %w", target, err)
	}

	shouldCleanup := true

	defer func() {
		if shouldCleanup {
			_, _ = process.Delete(ctx)
		}
	}()

	statusC, err := process.Wait(ctx)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec 대기 실패 %q: %w", target, err)
	}

	if err := process.Start(ctx); err != nil {
		return ExecResult{}, fmt.Errorf("exec 시작 실패 %q: %w", target, err)
	}

	if opts.Detach {
		shouldCleanup = false
		return ExecResult{}, nil
	}

	exitStatus := <-statusC
	code, _, err := exitStatus.Result()

	if err != nil {
		return ExecResult{}, fmt.Errorf(
			"exec 종료 상태 확인 실패 %q: %w",
			target,
			err)
	}
	return ExecResult{
		ExitCode: code,
	}, nil

}

type PullOptions struct {
	Namespace      string
	TargetPlatform string
	Snapshotter    string
}

func (r *Runtime) Pull(
	ctx context.Context,
	ref string,
	opts PullOptions,
) error {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)

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
	if opts.TargetPlatform != "" {
		p, err := platforms.Parse(opts.TargetPlatform)
		if err != nil {
			return fmt.Errorf("잘못된 플랫폼 형식 %q: %w", opts.TargetPlatform, err)
		}
		pullOpts = append(pullOpts, client.WithPlatformMatcher(platforms.Only(p)))
	} else {
		pullOpts = append(pullOpts, client.WithPlatformMatcher(platforms.Default()))
	}

	// 추가 옵션 2
	// 이미지 파일 시스템 설정 (native, btrfs,stargz 등)
	// 기본값은 overlayfs
	if opts.Snapshotter != "" {
		pullOpts = append(pullOpts, client.WithPullSnapshotter(opts.Snapshotter))
	}

	_, err := r.client.Pull(ctx, ref, pullOpts...)

	// 에러
	if err != nil {
		return fmt.Errorf("이미지 다운로드 실패 %q: %w", ref, err)
	}

	return nil
}
