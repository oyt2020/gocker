package runtime

import (
	"errors"
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

// Rollback : 등록된 롤백 함수를 역순으로 실행
func (r *RollbackTracker) Rollback() error {
	var errs []error
	for i := len(r.actions) - 1; i >= 0; i-- {
		if err := r.actions[i](); err != nil {
			errs = append(errs, err)
		}
	}
	r.actions = nil
	if len(errs) > 0 {
		return fmt.Errorf("errors during rollback: %w", errors.Join(errs...))
	}
	return nil
}

// Commit : 등록된 롤백 함수 모두 정리하여 롤백을 방지
func (r *RollbackTracker) Commit() {
	r.actions = nil
}
