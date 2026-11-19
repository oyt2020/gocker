package runc

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound runc가 해당 ID의 컨테이너를 알지 못함 (만들어진 적이 없거나 이미 delete됨)
var ErrNotFound = errors.New("runc container does not exist")

// notFoundMarker runc(libcontainer)가 없는 컨테이너에 대해 stderr로 출력하는 문구
const notFoundMarker = "does not exist"

// commandError : runc 명령 실패를 호출자가 원인별로 구분할 수 있는 에러로 변환한다.
//
//   - ctx가 취소/만료된 상태면 ctx.Err()를 감싼다. 이때의 실패는 runc가 아니라 호출자가 명령을 중단시킨
//     결과이므로 컨테이너 상태에 대해 아무것도 말해 주지 않는다 (errors.Is(err, context.Canceled) 등으로 구분).
//   - 대상 컨테이너가 없으면 ErrNotFound를 감싼다.
//   - 그 외에는 원래 에러와 runc의 stderr를 그대로 담는다.
func commandError(ctx context.Context, op string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("runc %s interrupted: %w", op, ctxErr)
	}
	if strings.Contains(stderr, notFoundMarker) {
		return fmt.Errorf("runc %s: %w: %s", op, ErrNotFound, stderr)
	}
	return fmt.Errorf("runc %s failed (%w): %s", op, err, stderr)
}
