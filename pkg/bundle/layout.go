package bundle

import (
	"path/filepath"
)

const (
	// DefaultGockerRoot : 기본 gocker 저장소 경로
	DefaultGockerRoot = "/var/lib/gocker"

	// ContainersDirName : Bundle 디렉터리가 저장되는 디렉터리 이름
	ContainersDirName = "containers"

	// RootFSDirName : Bundle 디렉터리에 저장되는 RootFS 디렉터리 이름 (OCI Bundle 주요 폴더)
	RootFSDirName = "rootfs"

	// ConfigFileName : 컨테이너 설정 저장하는 파일 이름 (OCI Bundle 주요 파일)
	ConfigFileName = "config.json"

	// MetadataFileName : 메타데이터 저장하는 파일 이름
	MetadataFileName = "metadata.json"

	// PidFileName : PID 저장 파일
	PidFileName = "container.pid"
)

// Layout : 컨테이너 파일 및 디렉터리 경로 관리
type Layout struct {
	rootDir string
}

// NewLayout : 기본 디렉터리 경로 생성, 기본 값 (DefaultGockerRoot)
func NewLayout(rootDir string) *Layout {
	if rootDir == "" {
		rootDir = DefaultGockerRoot
	}
	return &Layout{rootDir: rootDir}
}

// ContainersDir : /var/lib/gocker/containers 반환
func (l *Layout) ContainersDir() string {
	return filepath.Join(l.rootDir, ContainersDirName)
}

// BundleDir : /var/lib/gocker/containers/<container-id> 반환
func (l *Layout) BundleDir(id string) string {
	return filepath.Join(l.ContainersDir(), id)
}

// RootFSDir : /var/lib/gocker/containers/<container-id>/rootfs 반환
func (l *Layout) RootFSDir(id string) string {
	return filepath.Join(l.BundleDir(id), RootFSDirName)
}

// ConfigPath : /var/lib/gocker/containers/<container-id>/config.json 반환
func (l *Layout) ConfigPath(id string) string {
	return filepath.Join(l.BundleDir(id), ConfigFileName)
}

// MetadataPath : /var/lib/gocker/containers/<container-id>/metadata.json 반환
func (l *Layout) MetadataPath(id string) string {
	return filepath.Join(l.BundleDir(id), MetadataFileName)
}

// PidFilePath : /var/lib/gocker/containers/<container-id>/container.pid 반환
func (l *Layout) PidFilePath(id string) string {
	return filepath.Join(l.BundleDir(id), PidFileName)
}
