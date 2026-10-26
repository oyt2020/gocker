package bundle

import (
	"path/filepath"
)

const (
	// DefaultGockerRoot is the default storage path for Gocker in Linux environments.
	DefaultGockerRoot = "/var/lib/gocker"

	// ContainersDirName is the directory name where container bundles reside.
	ContainersDirName = "containers"

	// RootFSDirName is the rootfs directory inside a bundle.
	RootFSDirName = "rootfs"

	// ConfigFileName is the OCI bundle specification file.
	ConfigFileName = "config.json"

	// MetadataFileName is Gocker's container metadata file.
	MetadataFileName = "metadata.json"

	// PidFileName is the runc pid-file name.
	PidFileName = "container.pid"
)

// Layout manages file and directory paths for Gocker containers.
type Layout struct {
	rootDir string
}

// NewLayout creates a Layout with the given base directory (defaults to DefaultGockerRoot if empty).
func NewLayout(rootDir string) *Layout {
	if rootDir == "" {
		rootDir = DefaultGockerRoot
	}
	return &Layout{rootDir: rootDir}
}

// ContainersDir returns the path to /var/lib/gocker/containers
func (l *Layout) ContainersDir() string {
	return filepath.Join(l.rootDir, ContainersDirName)
}

// BundleDir returns the path to a specific container's bundle directory.
func (l *Layout) BundleDir(id string) string {
	return filepath.Join(l.ContainersDir(), id)
}

// RootFSDir returns the path to the rootfs directory inside a container bundle.
func (l *Layout) RootFSDir(id string) string {
	return filepath.Join(l.BundleDir(id), RootFSDirName)
}

// ConfigPath returns the path to config.json inside a container bundle.
func (l *Layout) ConfigPath(id string) string {
	return filepath.Join(l.BundleDir(id), ConfigFileName)
}

// MetadataPath returns the path to metadata.json inside a container bundle.
func (l *Layout) MetadataPath(id string) string {
	return filepath.Join(l.BundleDir(id), MetadataFileName)
}

// PidFilePath returns the path to the pid file inside a container bundle.
func (l *Layout) PidFilePath(id string) string {
	return filepath.Join(l.BundleDir(id), PidFileName)
}
