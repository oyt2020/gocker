package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ContainerState 컨테이너 라이프사이클
type ContainerState string

const (
	StateCreated ContainerState = "CREATED"
	StateRunning ContainerState = "RUNNING"
	StateStopped ContainerState = "STOPPED"
	StateDeleted ContainerState = "DELETED"
)

// ContainerMetadata 컨테이너 메타데이터
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

// SaveMetadata ContainerMetadata를 이용해서 metadata.json 파일 생성 및 저장
func SaveMetadata(meta *ContainerMetadata, path string) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal container metadata: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write container metadata to %s: %w", path, err)
	}
	return nil
}

// LoadMetadata metadata.json을 파싱하여 ContainerMetadata 반환
func LoadMetadata(path string) (*ContainerMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read container metadata %s: %w", path, err)
	}
	var meta ContainerMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal container metadata %s: %w", path, err)
	}
	return &meta, nil
}
