package runc

import "time"

// RuncState : `runc state <container-id>` 반환하는 JSON 구조
type RuncState struct {
	Version     string            `json:"ociVersion"`
	ID          string            `json:"id"`
	Status      string            `json:"status"` // "creating", "created", "running", "stopped"
	Pid         int               `json:"pid"`
	Bundle      string            `json:"bundle"`
	Rootfs      string            `json:"rootfs"`
	Created     time.Time         `json:"created"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

const (
	// StatusCreating : 컨테이너 생성 중
	StatusCreating = "creating"
	// StatusCreated : 컨테이너 생성 완료 / 아직 실행 전
	StatusCreated = "created"
	// StatusRunning : 컨테이너 실행 중
	StatusRunning = "running"
	// StatusStopped : 컨테이너 중지 됨
	StatusStopped = "stopped"
)
