package bundle

import (
	"fmt"
	"os"

	"github.com/oyt2020/gocker/pkg/util"
)

// Bundle represents an OCI bundle filesystem layout.
type Bundle struct {
	ID          string
	Path        string // Base bundle path (e.g. /var/lib/gocker/containers/<id>)
	RootFS      string // Root filesystem path (e.g. /var/lib/gocker/containers/<id>/rootfs)
	ConfigPath  string // Path to config.json
	MetaPath    string // Path to metadata.json
	PidFilePath string // Path to container.pid
}

// BundleManager defines the lifecycle operations for OCI container bundle directories.
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

// NewBundleManager creates a BundleManager using the specified Layout.
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

// Create creates a bundle directory, unpacks the RootFS tarball if specified, and returns the Bundle.
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
			_ = os.RemoveAll(b.Path) // Rollback newly created directory on failure
			return nil, fmt.Errorf("unpack rootfs archive: %w", err)
		}
	}

	return b, nil
}

// Delete removes the bundle directory for the specified container ID.
func (m *bundleManager) Delete(id string) error {
	bDir := m.layout.BundleDir(id)
	if err := os.RemoveAll(bDir); err != nil {
		return fmt.Errorf("delete bundle %q: %w", id, err)
	}
	return nil
}

// Exists checks if a bundle directory exists for the specified container ID.
func (m *bundleManager) Exists(id string) bool {
	bDir := m.layout.BundleDir(id)
	fi, err := os.Stat(bDir)
	return err == nil && fi.IsDir()
}

// Get returns the Bundle object if the container bundle directory exists.
func (m *bundleManager) Get(id string) (*Bundle, error) {
	if !m.Exists(id) {
		return nil, fmt.Errorf("container %q not found", id)
	}
	return m.bundleFor(id), nil
}

// List returns a list of all existing container bundle IDs.
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
