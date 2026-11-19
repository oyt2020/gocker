package runtime

import "github.com/oyt2020/gocker/pkg/store"

// runtime 패키지는 CLI에 대한 API 경계다. 메타데이터 모델과 상태 머신은 store 패키지가 소유하고,
// CLI가 store를 직접 알 필요가 없도록 runtime에서 별칭으로 노출한다.

// ContainerMetadata 컨테이너 메타데이터 (store.ContainerMetadata 별칭)
type ContainerMetadata = store.ContainerMetadata

// ContainerState 컨테이너 라이프사이클 상태 (store.ContainerState 별칭)
type ContainerState = store.ContainerState

const (
	StateCreated = store.StateCreated
	StateRunning = store.StateRunning
	StateStopped = store.StateStopped
	StateDeleted = store.StateDeleted
)
