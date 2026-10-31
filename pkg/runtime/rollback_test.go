package runtime

import (
	"reflect"
	"testing"
)

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
