package runc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RuncClient runc CLI 관리 인터페이스
type RuncClient interface {
	Create(ctx context.Context, id, bundleDir, pidFile string, ioOpts *IOOption) error
	Start(ctx context.Context, id string) error
	Kill(ctx context.Context, id, signal string) error
	Delete(ctx context.Context, id string, force bool) error
	State(ctx context.Context, id string) (*RuncState, error)
	Run(ctx context.Context, id, bundleDir, pidFile string, ioOpts *IOOption) error
}

type runcClient struct {
	binaryPath string
}

// NewRuncClient RuncClient 인스턴스 생성 함수 (파라미터 : runc 바이너리 경로)
func NewRuncClient(binaryPath string) RuncClient {
	if binaryPath == "" {
		binaryPath = "runc"
	}
	return &runcClient{binaryPath: binaryPath}
}

// Create : `runc create --bundle <bundleDir> [--pid-file <pidFile>] [--console-socket <sock>] <id>` 명령어 실행
func (r *runcClient) Create(ctx context.Context, id, bundleDir, pidFile string, ioOpts *IOOption) error {
	runcLogPath := filepath.Join(bundleDir, "runc.log")
	if ioOpts != nil && ioOpts.RuncLogPath != "" {
		runcLogPath = ioOpts.RuncLogPath
	}

	// runc 글로벌 옵션: runc --log <runcLogPath> create ...
	args := []string{"--log", runcLogPath, "create", "--bundle", bundleDir}
	if pidFile != "" {
		args = append(args, "--pid-file", pidFile)
	}
	if ioOpts != nil && ioOpts.ConsoleSocket != "" {
		args = append(args, "--console-socket", ioOpts.ConsoleSocket)
	}
	args = append(args, id)

	cmd := exec.CommandContext(ctx, r.binaryPath, args...)

	if ioOpts != nil {
		if ioOpts.Stdin != nil {
			cmd.Stdin = ioOpts.Stdin
		}
		if ioOpts.Stdout != nil {
			cmd.Stdout = ioOpts.Stdout
		}
		if ioOpts.Stderr != nil {
			// *os.File을 직접 연결하여 익명 파이프 생성 및 runc init FD 상속 데드락을 원천 방지
			cmd.Stderr = ioOpts.Stderr
		}
	}

	if err := cmd.Run(); err != nil {
		// runc create 실패 시 runc 자체 진단 로그 파일(--log)을 읽어 에러에 포함
		if logData, readErr := os.ReadFile(runcLogPath); readErr == nil && len(logData) > 0 {
			return fmt.Errorf("runc create failed (%w): %s", err, strings.TrimSpace(string(logData)))
		}
		return fmt.Errorf("runc create failed: %w", err)
	}
	return nil
}

// Start : `runc start <id>` 명령어 실행
func (r *runcClient) Start(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, r.binaryPath, "start", id)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runc start failed (%w): %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Kill : `runc kill <id> <signal>` 실행
func (r *runcClient) Kill(ctx context.Context, id, signal string) error {
	if signal == "" {
		signal = "SIGTERM"
	}
	cmd := exec.CommandContext(ctx, r.binaryPath, "kill", id, signal)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runc kill failed (%w): %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Delete : `runc delete [--force] <id>` 명령어 실행
func (r *runcClient) Delete(ctx context.Context, id string, force bool) error {
	args := []string{"delete"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, id)

	cmd := exec.CommandContext(ctx, r.binaryPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runc delete failed (%w): %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// State : `runc state <id>` 명령어 실행
func (r *runcClient) State(ctx context.Context, id string) (*RuncState, error) {
	cmd := exec.CommandContext(ctx, r.binaryPath, "state", id)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("runc state failed (%w): %s", err, strings.TrimSpace(stderr.String()))
	}

	var state RuncState
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		return nil, fmt.Errorf("parse runc state JSON: %w", err)
	}

	return &state, nil
}

// Run : `runc run --bundle <bundleDir> [--pid-file <pidFile>] [--console-socket <sock>] <id>` 명령어 실행
func (r *runcClient) Run(ctx context.Context, id, bundleDir, pidFile string, ioOpts *IOOption) error {
	runcLogPath := filepath.Join(bundleDir, "runc.log")
	if ioOpts != nil && ioOpts.RuncLogPath != "" {
		runcLogPath = ioOpts.RuncLogPath
	}

	args := []string{"--log", runcLogPath, "run", "--bundle", bundleDir}
	if pidFile != "" {
		args = append(args, "--pid-file", pidFile)
	}
	if ioOpts != nil && ioOpts.ConsoleSocket != "" {
		args = append(args, "--console-socket", ioOpts.ConsoleSocket)
	}
	args = append(args, id)

	cmd := exec.CommandContext(ctx, r.binaryPath, args...)

	if ioOpts != nil {
		cmd.Stdin = ioOpts.Stdin
		cmd.Stdout = ioOpts.Stdout
		cmd.Stderr = ioOpts.Stderr
	}

	if err := cmd.Run(); err != nil {
		if logData, readErr := os.ReadFile(runcLogPath); readErr == nil && len(logData) > 0 {
			return fmt.Errorf("runc run failed (%w): %s", err, strings.TrimSpace(string(logData)))
		}
		return fmt.Errorf("runc run failed: %w", err)
	}
	return nil
}
