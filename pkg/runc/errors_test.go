package runc

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCommandError_NotFound(t *testing.T) {
	err := commandError(context.Background(), "state", errors.New("exit status 1"),
		`time="2026-11-15T00:00:00Z" level=error msg="container does not exist"`+"\n")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCommandError_OtherFailureKeepsCauseAndStderr(t *testing.T) {
	cause := errors.New("exit status 1")
	err := commandError(context.Background(), "kill", cause, "permission denied\n")
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("unrelated failure must not be ErrNotFound: %v", err)
	}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("cause/stderr lost: %v", err)
	}
}

// ctx 취소로 중단된 명령의 실패는 stderr 내용과 무관하게 ctx 에러로 보고되어야 한다
func TestCommandError_ContextTakesPrecedence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := commandError(ctx, "state", errors.New("signal: killed"), "container does not exist")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("canceled command must not be reported as ErrNotFound: %v", err)
	}
}
