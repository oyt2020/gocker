package containerd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/term"
)

// Client containerd 클라이언트 래퍼 (Phase 1 로직 보존)
type Client struct {
	client *client.Client
}

// NewClient containerd 클라이언트 생성
func NewClient(socket string) (*Client, error) {
	if socket == "" {
		socket = "/run/containerd/containerd.sock"
	}
	c, err := client.New(socket)
	if err != nil {
		return nil, fmt.Errorf("connect containerd: %w", err)
	}

	return &Client{
		client: c,
	}, nil
}

// Close containerd 클라이언트 리소스 해제
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// RawClient 내부 containerd.Client 접근자
func (c *Client) RawClient() *client.Client {
	return c.client
}

// Version containerd 버전 확인
func (c *Client) Version(ctx context.Context) (client.Version, error) {
	return c.client.Version(ctx)
}

// ExecOptions 컨테이너 내부 명령어 실행 옵션
type ExecOptions struct {
	Namespace   string
	TTY         bool
	Interactive bool
	Detach      bool
}

// ExecResult 컨테이너 내부 명령어 실행 결과
type ExecResult struct {
	ExitCode uint32
}

// Exec 컨테이너 내부에서 새로운 프로세스 실행
func (c *Client) Exec(
	ctx context.Context,
	target string,
	cmdArgs []string,
	opts ExecOptions,
) (ExecResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)

	container, err := c.client.LoadContainer(ctx, target)
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
		return ExecResult{}, fmt.Errorf("exec 종료 상태 확인 실패 %q: %w", target, err)
	}
	return ExecResult{
		ExitCode: code,
	}, nil
}

// PullOptions 이미지 다운로드 옵션
type PullOptions struct {
	Namespace      string
	TargetPlatform string
	Snapshotter    string
}

// Pull 이미지 다운로드
func (c *Client) Pull(
	ctx context.Context,
	ref string,
	opts PullOptions,
) error {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)

	// 이미지 다운로드 받을 때 추가할 옵션 모음
	pullOpts := []client.RemoteOpt{
		client.WithPullUnpack,
		client.WithAllMetadata(),
	}

	if opts.TargetPlatform != "" {
		p, err := platforms.Parse(opts.TargetPlatform)
		if err != nil {
			return fmt.Errorf("잘못된 플랫폼 형식 %q: %w", opts.TargetPlatform, err)
		}
		pullOpts = append(pullOpts, client.WithPlatformMatcher(platforms.Only(p)))
	} else {
		pullOpts = append(pullOpts, client.WithPlatformMatcher(platforms.Default()))
	}

	if opts.Snapshotter != "" {
		pullOpts = append(pullOpts, client.WithPullSnapshotter(opts.Snapshotter))
	}

	_, err := c.client.Pull(ctx, ref, pullOpts...)
	if err != nil {
		return fmt.Errorf("이미지 다운로드 실패 %q: %w", ref, err)
	}

	return nil
}

// ImagesOptions 로컬 이미지 목록 조회 옵션
type ImagesOptions struct {
	Namespace   string
	ImgName     string
	ImgPlatform string
}

// ImageResult 이미지 조회 결과 정보
type ImageResult struct {
	Repository     string
	ImageID        string
	Size           string
	PlatformStatus string
}

// Images 로컬에 다운로드된 이미지 목록 조회
func (c *Client) Images(
	ctx context.Context,
	opts ImagesOptions,
) ([]ImageResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)

	imageList, err := c.client.ListImages(ctx)
	if err != nil {
		return []ImageResult{}, fmt.Errorf("이미지 목록 조회 실패 : %w", err)
	}

	var targetMatcher platforms.MatchComparer
	var parsedPlatform ocispec.Platform

	if opts.ImgPlatform != "" {
		p, err := platforms.Parse(opts.ImgPlatform)
		if err != nil {
			return []ImageResult{}, fmt.Errorf("잘못된 플랫폼 형식 %q: %w", opts.ImgPlatform, err)
		}
		parsedPlatform = p
		targetMatcher = platforms.Only(p)
	}

	hostPlatform := platforms.DefaultString()
	cs := c.client.ContentStore()
	var imgResults []ImageResult

	for _, img := range imageList {
		fullName := img.Name()

		if opts.ImgName != "" {
			if !strings.Contains(strings.ToLower(fullName), strings.ToLower(opts.ImgName)) {
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

			sizeStr = FormatBytes(targetSize)
			if platforms.Format(parsedPlatform) == hostPlatform {
				platformStatus = fmt.Sprintf("[일치] (호스트 %s)", hostPlatform)
			} else {
				platformStatus = fmt.Sprintf("[불일치] (%s / 호스트 : %s)", opts.ImgPlatform, hostPlatform)
			}
		} else {
			size, err := img.Size(ctx)
			if err == nil {
				sizeStr = FormatBytes(size)
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
									sizeStr = FormatBytes(s)
									platformStatus = fmt.Sprintf("[불일치] 다운로드: %s (호스트 : %s)", platforms.Format(*child.Platform), hostPlatform)
									break
								}
							}
						}
					}
				}
			}
		}
		imgResults = append(imgResults, ImageResult{
			Repository:     fullName,
			ImageID:        shortDigest,
			Size:           sizeStr,
			PlatformStatus: platformStatus,
		})
	}

	return imgResults, nil
}

// PsOptions 컨테이너 목록 조회 옵션
type PsOptions struct {
	Namespace string
	All       bool
}

// PsResult 컨테이너 목록 조회 결과
type PsResult struct {
	ContainerID string
	ImageName   string
	Pid         int64
	Status      string
}

// Ps containerd 컨테이너 목록 조회
func (c *Client) Ps(
	ctx context.Context,
	opts PsOptions,
) ([]PsResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)
	containers, err := c.client.Containers(ctx)
	if err != nil {
		return []PsResult{}, fmt.Errorf("컨테이너 조회 실패: %w", err)
	}

	var psResults []PsResult
	for _, cnt := range containers {
		info, err := cnt.Info(ctx)
		if err != nil {
			return []PsResult{}, fmt.Errorf("컨테이너 메타데이터 조회 실패: %w", err)
		}

		task, taskErr := cnt.Task(ctx, nil)
		var isRunning bool
		var statusStr string
		var pid int64

		if taskErr != nil {
			isRunning = false
			statusStr = "Exited"
			pid = -1
		} else {
			status, err := task.Status(ctx)
			if err == nil {
				switch status.Status {
				case client.Running:
					isRunning = true
					statusStr = "Running"
					pid = int64(task.Pid())
				case client.Stopped:
					isRunning = false
					statusStr = "Stopped"
				case client.Paused:
					isRunning = true
					statusStr = "Paused"
					pid = int64(task.Pid())
				default:
					statusStr = string(status.Status)
				}
			} else {
				statusStr = "Unknown"
			}
		}

		if !opts.All && !isRunning {
			continue
		}

		imageName := info.Image
		psResults = append(psResults, PsResult{
			ContainerID: cnt.ID(),
			ImageName:   imageName,
			Pid:         pid,
			Status:      statusStr,
		})
	}

	return psResults, nil
}

// StopOptions 컨테이너 중지 옵션
type StopOptions struct {
	Namespace string
	Timeout   int
}

// StopResult 컨테이너 중지 결과
type StopResult struct {
	ContainerID []string
}

// Stop containerd 컨테이너 중지
func (c *Client) Stop(
	ctx context.Context,
	targets []string,
	opts StopOptions,
) (StopResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)
	var results []string

	for _, target := range targets {
		container, err := c.client.LoadContainer(ctx, target)
		if err != nil {
			return StopResult{}, fmt.Errorf("컨테이너 찾기 실패 %q: %w", target, err)
		}

		task, err := container.Task(ctx, nil)
		if err != nil {
			return StopResult{}, fmt.Errorf("task 찾기 실패 %q : %w", target, err)
		}

		status, err := task.Status(ctx)
		if err != nil {
			return StopResult{}, fmt.Errorf("task의 상태 확인 실패 %q : %w", target, err)
		}

		if status.Status != client.Running {
			return StopResult{}, fmt.Errorf("task 상태가 실행중이 아님 %q : %s", target, status.Status)
		}

		exitStatusC, err := task.Wait(ctx)
		if err != nil {
			return StopResult{}, fmt.Errorf("컨테이너 대기 채널 생성 실패 %q : %w", target, err)
		}

		if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
			return StopResult{}, fmt.Errorf("SIGTERM 신호 전송 실패 %q : %w", target, err)
		}

		select {
		case <-exitStatusC:
		case <-time.After(time.Duration(opts.Timeout) * time.Second):
			if err := task.Kill(ctx, syscall.SIGKILL); err != nil {
				return StopResult{}, fmt.Errorf("SIGKILL 신호 전송 실패 %q : %w", target, err)
			}
			<-exitStatusC
		}

		_, err = task.Delete(ctx)
		if err != nil {
			return StopResult{}, fmt.Errorf("task 정리 실패 %q : %w", target, err)
		}

		results = append(results, container.ID())
	}
	return StopResult{
		ContainerID: results,
	}, nil
}

// StartOptions 컨테이너 시작 옵션
type StartOptions struct {
	Namespace string
	Attach    bool
}

// StartResult 컨테이너 시작 결과
type StartResult struct {
	ContainerID []string
	ExitCode    int64
}

// Start containerd 컨테이너 시작
func (c *Client) Start(
	ctx context.Context,
	targets []string,
	opts StartOptions,
) (StartResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)
	var results []string
	var exitCode int64

	for _, target := range targets {
		container, err := c.client.LoadContainer(ctx, target)
		if err != nil {
			return StartResult{}, fmt.Errorf("컨테이너 찾기 실패 %q: %w", target, err)
		}

		task, err := container.Task(ctx, nil)
		if err != nil {
			if !errdefs.IsNotFound(err) {
				return StartResult{}, fmt.Errorf("컨테이너로부터 task 확인 실패 %q : %w", target, err)
			}
		} else {
			status, sErr := task.Status(ctx)
			if sErr != nil {
				return StartResult{}, fmt.Errorf("task 상태 확인 실패 %q : %w", target, sErr)
			}

			switch status.Status {
			case client.Running:
				return StartResult{}, fmt.Errorf("이미 실행중인 Task %q : %s", target, status.Status)
			case client.Paused, client.Pausing, client.Unknown, client.Created:
				return StartResult{}, fmt.Errorf("현재 %s 상태인 Task, start 명령어 적용 불가능 %q", status.Status, target)
			case client.Stopped:
				_, err = task.Delete(ctx)
				if err != nil {
					return StartResult{}, fmt.Errorf("종료된 Task 정리 실패 %q : %w", target, err)
				}
			default:
				return StartResult{}, fmt.Errorf("처리할 수 없는 Task 상태 %q: %s", target, status.Status)
			}
		}

		var ioCreator cio.Creator
		if opts.Attach {
			spec, specErr := container.Spec(ctx)
			if specErr != nil {
				return StartResult{}, fmt.Errorf("spec 조회 실패 %q : %w", target, specErr)
			}

			cioOpts := []cio.Opt{cio.WithStdio}
			if spec.Process != nil && spec.Process.Terminal {
				cioOpts = append(cioOpts, cio.WithTerminal)
			}
			ioCreator = cio.NewCreator(cioOpts...)
		} else {
			ioCreator = cio.NullIO
		}

		newTask, err := container.NewTask(ctx, ioCreator)
		shouldCleanup := true
		if err != nil {
			return StartResult{}, fmt.Errorf("task 생성 실패 %q : %w", target, err)
		}

		defer func() {
			if shouldCleanup {
				_, _ = newTask.Delete(ctx)
			}
		}()

		if !opts.Attach {
			if err := newTask.Start(ctx); err != nil {
				return StartResult{}, fmt.Errorf("task 시작 실패 %q : %w", target, err)
			}
			shouldCleanup = false
			results = append(results, container.ID())
		} else {
			statusC, err := newTask.Wait(ctx)
			if err != nil {
				return StartResult{}, fmt.Errorf("task 대기 실패 %q : %w", target, err)
			}

			if err := newTask.Start(ctx); err != nil {
				return StartResult{}, fmt.Errorf("task 시작 실패 %q : %w", target, err)
			}

			status := <-statusC
			code, _, err := status.Result()
			if err != nil {
				return StartResult{}, fmt.Errorf("task 종료 상태 확인 실패 %q : %w", target, err)
			}

			_, err = newTask.Delete(ctx)
			if err != nil {
				return StartResult{}, fmt.Errorf("task 정리 실패 %q : %w", target, err)
			}

			results = append(results, container.ID())
			exitCode = int64(code)
			shouldCleanup = false
		}
	}

	return StartResult{
		ContainerID: results,
		ExitCode:    exitCode,
	}, nil
}

// RunOptions containerd 컨테이너 실행 옵션
type RunOptions struct {
	Namespace   string
	ContainerID string
	ImageRef    string
	Detach      bool
	Rm          bool
	TTY         bool
	Command     []string
}

// RunResult containerd 컨테이너 실행 결과
type RunResult struct {
	ContainerID string
	ExitCode    int64
}

// Run containerd 컨테이너 생성 및 실행
func (c *Client) Run(
	ctx context.Context,
	opts RunOptions,
) (RunResult, error) {
	ctx = namespaces.WithNamespace(ctx, opts.Namespace)
	var exitCode int64

	img, err := c.client.GetImage(ctx, opts.ImageRef)
	if err != nil {
		return RunResult{}, fmt.Errorf("이미지 찾기 실패 %q : %w", opts.ImageRef, err)
	}

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
	}

	if len(opts.Command) > 0 {
		specOpts = append(specOpts, oci.WithProcessArgs(opts.Command...))
	}

	if opts.ContainerID == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		opts.ContainerID = hex.EncodeToString(b)
	}

	if opts.TTY {
		specOpts = append(specOpts, oci.WithTTY)
	}

	container, err := c.client.NewContainer(
		ctx,
		opts.ContainerID,
		client.WithImage(img),
		client.WithNewSnapshot(opts.ContainerID, img),
		client.WithNewSpec(specOpts...),
	)
	if err != nil {
		return RunResult{}, fmt.Errorf("컨테이너 생성 실패 %q : %w", opts.ContainerID, err)
	}

	var ioCreator cio.Creator
	if opts.Detach {
		ioCreator = cio.NullIO
	} else {
		cioOpts := []cio.Opt{cio.WithStdio}
		if opts.TTY {
			cioOpts = append(cioOpts, cio.WithTerminal)
		}
		ioCreator = cio.NewCreator(cioOpts...)
	}

	task, err := container.NewTask(ctx, ioCreator)
	if err != nil {
		return RunResult{}, fmt.Errorf("task 생성 실패 %q : %w", opts.ContainerID, err)
	}

	if opts.Detach {
		if err := task.Start(ctx); err != nil {
			_, _ = task.Delete(ctx)
			_ = container.Delete(ctx, client.WithSnapshotCleanup)
			return RunResult{}, fmt.Errorf("task 실행 실패 %q : %w", opts.ContainerID, err)
		}

		if opts.Rm {
			_ = container.Delete(ctx, client.WithSnapshotCleanup)
		}

		return RunResult{
			ContainerID: opts.ContainerID,
			ExitCode:    exitCode,
		}, nil
	}

	code, err := c.startOne(ctx, container, task, opts)
	if err != nil {
		return RunResult{}, err
	}

	if opts.Rm {
		err := container.Delete(ctx, client.WithSnapshotCleanup)
		if err != nil {
			return RunResult{}, fmt.Errorf("컨테이너 메타데이터 삭제 실패 %q : %w", opts.ContainerID, err)
		}
	}

	return RunResult{
		ContainerID: opts.ContainerID,
		ExitCode:    code,
	}, nil
}

func (c *Client) startOne(ctx context.Context, container client.Container, task client.Task, opts RunOptions) (exitCode int64, retErr error) {
	shouldContainerCleanup := true

	defer func() {
		if _, taskDeleteErr := task.Delete(ctx); taskDeleteErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("task 정리 실패 %q : %w", opts.ContainerID, taskDeleteErr))
			return
		}

		if shouldContainerCleanup {
			containerDeleteErr := container.Delete(ctx, client.WithSnapshotCleanup)
			if containerDeleteErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("컨테이너 메타데이터 Rollback 실패 %q : %w", opts.ContainerID, containerDeleteErr))
			}
		}
	}()

	statusC, err := task.Wait(ctx)
	if err != nil {
		return -1, fmt.Errorf("task 대기 실패 %q : %w", opts.ContainerID, err)
	}

	if err := task.Start(ctx); err != nil {
		return -1, fmt.Errorf("task 시작 실패 %q : %w", opts.ContainerID, err)
	}

	shouldContainerCleanup = false

	status := <-statusC
	code, _, err := status.Result()
	if err != nil {
		return -1, fmt.Errorf("task 종료 상태 확인 실패 %q : %w", opts.ContainerID, err)
	}

	return int64(code), nil
}

// FormatBytes 바이트 크기를 사람이 읽기 쉬운 단위로 변환
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func generateRandomID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
