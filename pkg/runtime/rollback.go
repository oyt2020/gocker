package runtime

import (
	"fmt"
)

// RollbackTracker : LIFO 순서로 롤백 함수 추적
type RollbackTracker struct {
	actions []func() error
}

// NewRollbackTracker : RollbackTracker 생성
func NewRollbackTracker() *RollbackTracker {
	return &RollbackTracker{
		actions: make([]func() error, 0),
	}
}

// Add : 롤백 함수 역순으로 추가
func (r *RollbackTracker) Add(action func() error) {
	if action != nil {
		r.actions = append(r.actions, action)
	}
}

// Rollback : 등록된 롤백 함수를 역순으로 실행한다.
//
// 하나가 실패하면 거기서 멈추고, 그보다 먼저 등록된(=먼저 만들어진) 리소스는 그대로 남긴다.
// 나중에 만든 리소스를 치우지 못했는데 그것을 가리키는 앞선 리소스(예: 컨테이너 레코드)까지 지워 버리면,
// 남은 리소스를 다시 찾아 정리할 방법이 사라지기 때문이다.
func (r *RollbackTracker) Rollback() error {
	actions := r.actions
	r.actions = nil

	for i := len(actions) - 1; i >= 0; i-- {
		if err := actions[i](); err != nil {
			return fmt.Errorf("rollback stopped with %d earlier step(s) not undone: %w", i, err)
		}
	}
	return nil
}

// Commit : 등록된 롤백 함수 모두 정리하여 롤백을 방지
func (r *RollbackTracker) Commit() {
	r.actions = nil
}
