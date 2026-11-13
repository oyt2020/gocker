package store

import (
	"fmt"
	"time"
)

// ContainerState 컨테이너 라이프사이클 상태
type ContainerState string

const (
	StateCreated ContainerState = "CREATED"
	StateRunning ContainerState = "RUNNING"
	StateStopped ContainerState = "STOPPED"
	// StateDeleted 삭제는 번들 디렉터리 제거로 표현되며 metadata.json에 저장되지 않는다
	StateDeleted ContainerState = "DELETED"
)

// allowedTransitions 상태 머신 정의 (현재 상태 → 이동 가능한 다음 상태)
//
//	CREATED ──start──▶ RUNNING ──stop/exit──▶ STOPPED
//	   └────────────kill/exit────────────────▲
var allowedTransitions = map[ContainerState][]ContainerState{
	StateCreated: {StateRunning, StateStopped},
	StateRunning: {StateStopped},
	StateStopped: {},
}

// CanTransitionTo : 현재 상태에서 next로 전환 가능한지 여부 (같은 상태로의 전환은 허용)
func (s ContainerState) CanTransitionTo(next ContainerState) bool {
	if s == next {
		return true
	}
	for _, allowed := range allowedTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// ContainerMetadata 컨테이너 메타데이터 (metadata.json)
type ContainerMetadata struct {
	ID         string         `json:"id"`
	Name       string         `json:"name,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	Image      string         `json:"image"`
	Command    []string       `json:"command"`
	BundlePath string         `json:"bundlePath"`
	State      ContainerState `json:"state"`
	Pid        int            `json:"pid"`
	ExitCode   *int           `json:"exitCode,omitempty"`
}

// 상태 변경은 아래 두 메서드로만 일어난다.
//   - TransitionTo   : gocker가 의도한 명령(start/stop 등)에 의한 변경. 상태 머신 규칙을 강제한다.
//   - ApplyObserved  : 하위 런타임(runc)에서 관측한 실제 상태 반영. 실행 중 여부의 원천(source of truth)은
//                      하위 런타임이므로 규칙을 강제하지 않는다 (예: 저장된 STOPPED인데 실제로는 아직 실행 중).

// TransitionTo : 상태 머신 규칙을 검사한 뒤 상태를 변경한다.
func (m *ContainerMetadata) TransitionTo(next ContainerState) error {
	if !m.State.CanTransitionTo(next) {
		return fmt.Errorf("container %q: invalid state transition %s -> %s", m.ID, m.State, next)
	}
	m.setState(next, m.Pid)
	return nil
}

// ApplyObserved : 하위 런타임에서 관측한 상태와 PID를 반영한다 (메모리 상의 뷰만 변경, 저장은 호출자 책임).
func (m *ContainerMetadata) ApplyObserved(observed ContainerState, pid int) {
	m.setState(observed, pid)
}

func (m *ContainerMetadata) setState(next ContainerState, pid int) {
	m.State = next
	if next == StateRunning {
		m.Pid = pid
	} else {
		m.Pid = 0
	}
}
