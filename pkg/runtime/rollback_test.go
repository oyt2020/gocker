package runtime

import (
	"errors"
	"reflect"
	"testing"
)

// 정리가 실패하면 거기서 멈추고, 먼저 등록된 단계는 실행하지 않는다 (남은 리소스를 가리키는 앞선 리소스를 보존)
func TestRollbackTracker_StopsAtFirstFailure(t *testing.T) {
	tracker := NewRollbackTracker()
	boom := errors.New("boom")

	var order []int
	tracker.Add(func() error {
		order = append(order, 1)
		return nil
	})
	tracker.Add(func() error {
		order = append(order, 2)
		return boom
	})
	tracker.Add(func() error {
		order = append(order, 3)
		return nil
	})

	if err := tracker.Rollback(); !errors.Is(err, boom) {
		t.Fatalf("rollback error = %v, want it to wrap boom", err)
	}
	if want := []int{3, 2}; !reflect.DeepEqual(order, want) {
		t.Errorf("executed steps = %v, want %v", order, want)
	}

	// 한 번 실행한 뒤에는 남은 단계를 다시 실행하지 않는다
	if err := tracker.Rollback(); err != nil {
		t.Fatalf("second rollback must be a no-op, got %v", err)
	}
	if len(order) != 2 {
		t.Errorf("steps re-executed on second rollback: %v", order)
	}
}

func TestRollbackTracker(t *testing.T) {
	tracker := NewRollbackTracker()

	var order []int

	tracker.Add(func() error {
		order = append(order, 1)
		return nil
	})
	tracker.Add(func() error {
		order = append(order, 2)
		return nil
	})
	tracker.Add(func() error {
		order = append(order, 3)
		return nil
	})

	if err := tracker.Rollback(); err != nil {
		t.Fatalf("unexpected rollback error: %v", err)
	}

	expectedOrder := []int{3, 2, 1}
	if !reflect.DeepEqual(order, expectedOrder) {
		t.Errorf("expected LIFO order %v, got %v", expectedOrder, order)
	}

	// Test Commit
	tracker2 := NewRollbackTracker()
	called := false
	tracker2.Add(func() error {
		called = true
		return nil
	})
	tracker2.Commit()
	_ = tracker2.Rollback()

	if called {
		t.Errorf("expected committed action not to be called during rollback")
	}
}
