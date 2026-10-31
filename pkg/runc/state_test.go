package runc

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRuncStateUnmarshal(t *testing.T) {
	rawJSON := `{
		"ociVersion": "1.0.2",
		"id": "container123",
		"status": "running",
		"pid": 4821,
		"bundle": "/var/lib/gocker/containers/container123",
		"rootfs": "/var/lib/gocker/containers/container123/rootfs",
		"created": "2026-10-26T15:00:00.123456Z"
	}`

	var state RuncState
	if err := json.Unmarshal([]byte(rawJSON), &state); err != nil {
		t.Fatalf("failed to unmarshal runc state: %v", err)
	}

	if state.ID != "container123" {
		t.Errorf("expected ID container123, got %s", state.ID)
	}
	if state.Status != StatusRunning {
		t.Errorf("expected status %s, got %s", StatusRunning, state.Status)
	}
	if state.Pid != 4821 {
		t.Errorf("expected PID 4821, got %d", state.Pid)
	}
	expectedTime, _ := time.Parse(time.RFC3339Nano, "2026-10-26T15:00:00.123456Z")
	if !state.Created.Equal(expectedTime) {
		t.Errorf("expected created time %v, got %v", expectedTime, state.Created)
	}
}
