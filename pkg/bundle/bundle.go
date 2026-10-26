package bundle

import (
	"fmt"
	"os"

	"github.com/oyt2020/gocker/pkg/util"
)

// OCI Bundle 파일 시스템 레이아웃
type Bundle struct {
	ID          string
	Path        string // 기본 Bundle 디렉터리 경로 (e.g. /var/lib/gocker/containers/<id>)
	RootFS      string // RootFS 경로 (e.g. /var/lib/gocker/containers/<id>/rootfs)
	ConfigPath  string // config.json 경로
	MetaPath    string // metadata.json 경로
	PidFilePath string // container.pid 경로
}

// BundleManager : Bundle 디렉터리 관리 인터페이스
type BundleManager interface {
	Create(id string, rootfsTarPath string) (*Bundle, error)
	Delete(id string) error
	Exists(id string) bool
	Get(id string) (*Bundle, error)
	List() ([]string, error)
	Layout() *Layout
}

type bundleManager struct {
	layout *Layout
}

// NewBundleManager : Layout 구조체를 이용해 BundleManager 생성 및 반환
func NewBundleManager(layout *Layout) BundleManager {
	if layout == nil {
		layout = NewLayout("")
	}
	return &bundleManager{layout: layout}
}

func (m *bundleManager) Layout() *Layout {
	return m.layout
}

func (m *bundleManager) bundleFor(id string) *Bundle {
	return &Bundle{
		ID:          id,
		Path:        m.layout.BundleDir(id),
		RootFS:      m.layout.RootFSDir(id),
		ConfigPath:  m.layout.ConfigPath(id),
		MetaPath:    m.layout.MetadataPath(id),
		PidFilePath: m.layout.PidFilePath(id),
	}
}

// Create : bundle 디렉터리 생성 및 RootFS 압축 해제 및 적용
func (m *bundleManager) Create(id string, rootfsTarPath string) (*Bundle, error) {
	if id == "" {
		return nil, fmt.Errorf("container id cannot be empty")
	}

	b := m.bundleFor(id)

	if m.Exists(id) {
		return nil, fmt.Errorf("bundle for container %q already exists at %s", id, b.Path)
	}

	if err := os.MkdirAll(b.RootFS, 0755); err != nil {
		return nil, fmt.Errorf("create bundle rootfs dir: %w", err)
	}

	if rootfsTarPath != "" {
		if err := util.ExtractTarGz(rootfsTarPath, b.RootFS); err != nil {
			_ = os.RemoveAll(b.Path) // 실패 시 롤백
			return nil, fmt.Errorf("unpack rootfs archive: %w", err)
		}
	}

	return b, nil
}

// Delete : container ID로 Bundle 디렉터리 삭제
func (m *bundleManager) Delete(id string) error {
	bDir := m.layout.BundleDir(id)
	if err := os.RemoveAll(bDir); err != nil {
		return fmt.Errorf("delete bundle %q: %w", id, err)
	}
	return nil
}

// Exists : container ID로 Bundle 디렉터리 존재하는 지 확인
func (m *bundleManager) Exists(id string) bool {
	bDir := m.layout.BundleDir(id)
	fi, err := os.Stat(bDir)
	return err == nil && fi.IsDir()
}

// Get : Exists로 Bundle 디렉터리 존재 확인 후 존재하면 Bundle 객체 반환
func (m *bundleManager) Get(id string) (*Bundle, error) {
	if !m.Exists(id) {
		return nil, fmt.Errorf("container %q not found", id)
	}
	return m.bundleFor(id), nil
}

// List : 모든 Bundle 디렉터리 ID 목록 반환
func (m *bundleManager) List() ([]string, error) {
	cDir := m.layout.ContainersDir()
	entries, err := os.ReadDir(cDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read containers directory: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}
