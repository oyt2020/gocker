package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oyt2020/gocker/pkg/store"
	"github.com/oyt2020/gocker/pkg/util"
)

// minIDPrefixLen ID 접두어로 인정하는 최소 길이 (너무 짧은 접두어로 엉뚱한 컨테이너가 선택되는 것을 방지)
const minIDPrefixLen = 4

// 아래 공개 메서드들은 컨테이너 참조(ref: 전체 ID / ID 접두어 / 이름)를 받는다.
// 참조를 전체 ID로 해석한 뒤, 정확한 ID만 다루는 내부 구현(start / stop / remove / get)에 위임한다.
// 호출자(CLI)는 사용자가 입력한 문자열을 그대로 넘기면 된다.

// Start : ref가 가리키는 컨테이너를 시작한다
func (r *runcRuntime) Start(ctx context.Context, ref string) error {
	id, err := r.resolveID(ref)
	if err != nil {
		return err
	}
	return r.start(ctx, id)
}

// Stop : ref가 가리키는 컨테이너를 중지한다 (SIGTERM 후 timeout이 지나면 SIGKILL)
func (r *runcRuntime) Stop(ctx context.Context, ref string, timeout time.Duration) error {
	id, err := r.resolveID(ref)
	if err != nil {
		return err
	}
	return r.stop(ctx, id, timeout)
}

// Delete : ref가 가리키는 컨테이너를 삭제한다
func (r *runcRuntime) Delete(ctx context.Context, ref string, force bool) error {
	id, err := r.resolveID(ref)
	if err != nil {
		return err
	}
	return r.remove(ctx, id, force)
}

// Get : ref가 가리키는 컨테이너의 메타데이터를 runc 관측 상태와 함께 반환한다 (읽기 전용)
func (r *runcRuntime) Get(ctx context.Context, ref string) (*ContainerMetadata, error) {
	id, err := r.resolveID(ref)
	if err != nil {
		return nil, err
	}
	return r.get(ctx, id)
}

// resolveID : 컨테이너 참조를 전체 ID로 변환한다.
//
// 우선순위: 전체 ID 일치 → 이름 일치 또는 ID 접두어(minIDPrefixLen 이상) 일치.
// 후보가 여럿이면 임의로 고르지 않고 에러를 반환한다. 찾지 못하면 store.ErrNotFound를 감싼 에러를 반환한다.
// 저장된 레코드만 보므로 runc 조회가 없고, 반환된 ID는 항상 유효한 형식이라 경로 조작에 쓰일 수 없다.
//
// 해석은 컨테이너 잠금 밖에서 일어난다. 해석 직후 컨테이너가 삭제되면 이후 단계가 not found로 실패한다.
func (r *runcRuntime) resolveID(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("container id or name cannot be empty")
	}

	// 1. 전체 ID 형식이면 단건 조회 (목록 전체를 읽지 않는다)
	if util.IsValidID(ref) {
		if meta, err := r.store.Get(ref); err == nil {
			return meta.ID, nil
		}
	}

	// 2. 전체 목록에서 이름 또는 ID 접두어 검색
	metas, err := r.store.List()
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	var matches []string
	for _, m := range metas {
		if m.ID == ref {
			return m.ID, nil
		}
		byPrefix := len(ref) >= minIDPrefixLen && strings.HasPrefix(m.ID, ref)
		byName := m.Name != "" && m.Name == ref
		if byPrefix || byName {
			matches = append(matches, m.ID)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("container %q: %w", ref, store.ErrNotFound)
	default:
		return "", fmt.Errorf(
			"컨테이너 %q에 일치하는 대상이 %d개입니다. "+
				"'gocker ps -a'로 ID를 확인한 뒤 전체 컨테이너 ID로 다시 시도하세요",
			ref, len(matches),
		)
	}
}
