package bundle

import (
	"fmt"
	"os"
	"path/filepath"

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
	LogPath     string // container.log 경로
	RuncLogPath string // runc.log 경로
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

func (m *bundleManager) bundleFor(id string) (*Bundle, error) {
	if err := util.ValidateID(id); err != nil {
		return nil, err
	}
	return &Bundle{
		ID:          id,
		Path:        m.layout.BundleDir(id),
		RootFS:      m.layout.RootFSDir(id),
		ConfigPath:  m.layout.ConfigPath(id),
		MetaPath:    m.layout.MetadataPath(id),
		PidFilePath: m.layout.PidFilePath(id),
		LogPath:     m.layout.LogFilePath(id),
		RuncLogPath: m.layout.RuncLogPath(id),
	}, nil
}

func (m *bundleManager) openContainersRoot(createIfMissing bool) (*os.Root, error) {
	cDir := m.layout.ContainersDir()
	if createIfMissing {
		if err := os.MkdirAll(cDir, 0755); err != nil {
			return nil, fmt.Errorf("create containers dir: %w", err)
		}
	}
	return os.OpenRoot(cDir)
}

// Create : bundle 디렉터리 생성 및 RootFS 압축 해제 및 적용
func (m *bundleManager) Create(id string, rootfsTarPath string) (*Bundle, error) {
	b, err := m.bundleFor(id)
	if err != nil {
		return nil, err
	}

	if m.Exists(id) {
		return nil, fmt.Errorf("bundle for container %q already exists at %s", id, b.Path)
	}

	root, err := m.openContainersRoot(true)
	if err != nil {
		return nil, fmt.Errorf("open containers root: %w", err)
	}
	defer root.Close()

	if err := root.MkdirAll(filepath.Join(id, RootFSDirName), 0755); err != nil {
		return nil, fmt.Errorf("create bundle rootfs dir: %w", err)
	}

	if rootfsTarPath != "" {
		if err := util.ExtractTarGz(rootfsTarPath, b.RootFS); err != nil {
			_ = root.RemoveAll(id) // 실패 시 root 핸들을 통해 안전하게 롤백
			return nil, fmt.Errorf("unpack rootfs archive: %w", err)
		}
	}

	return b, nil
}

// Delete : container ID로 Bundle 디렉터리 삭제
func (m *bundleManager) Delete(id string) error {
	if _, err := m.bundleFor(id); err != nil {
		return err
	}

	root, err := m.openContainersRoot(false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open containers root: %w", err)
	}
	defer root.Close()

	if err := root.RemoveAll(id); err != nil {
		return fmt.Errorf("delete bundle %q: %w", id, err)
	}
	return nil
}

// Exists : container ID로 Bundle 디렉터리 존재하는 지 확인
func (m *bundleManager) Exists(id string) bool {
	if _, err := m.bundleFor(id); err != nil {
		return false
	}

	root, err := m.openContainersRoot(false)
	if err != nil {
		return false
	}
	defer root.Close()

	fi, err := root.Stat(id)
	return err == nil && fi.IsDir()
}

// Get : Exists로 Bundle 디렉터리 존재 확인 후 존재하면 Bundle 객체 반환
func (m *bundleManager) Get(id string) (*Bundle, error) {
	b, err := m.bundleFor(id)
	if err != nil {
		return nil, err
	}
	if !m.Exists(id) {
		return nil, fmt.Errorf("container %q not found", id)
	}
	return b, nil
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
		if entry.IsDir() && util.IsValidID(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}
