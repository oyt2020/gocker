package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/oyt2020/gocker/pkg/bundle"
	"github.com/oyt2020/gocker/pkg/util"
)

// ErrNotFound 컨테이너(번들 또는 메타데이터)가 존재하지 않음
var ErrNotFound = errors.New("container not found")

// ContainerStore 컨테이너 메타데이터 영속화 인터페이스
type ContainerStore interface {
	// Get : 저장된 메타데이터를 읽는다 (부작용 없음)
	Get(id string) (*ContainerMetadata, error)
	// Put : 메타데이터를 원자적으로 저장한다 (임시 파일 작성 후 rename)
	Put(meta *ContainerMetadata) error
	// Lock : 컨테이너 단위 배타 잠금(flock)을 획득한다. 다른 gocker 프로세스와의 동시 상태 변경을 직렬화한다.
	Lock(id string) (unlock func(), err error)
}

type fileStore struct {
	layout *bundle.Layout
}

// NewFileStore : 번들 디렉터리 안의 metadata.json을 사용하는 ContainerStore 생성
func NewFileStore(layout *bundle.Layout) ContainerStore {
	if layout == nil {
		layout = bundle.NewLayout("")
	}
	return &fileStore{layout: layout}
}

func (s *fileStore) Get(id string) (*ContainerMetadata, error) {
	if err := util.ValidateID(id); err != nil {
		return nil, err
	}
	path := s.layout.MetadataPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("container %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("read container metadata %s: %w", path, err)
	}
	var meta ContainerMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal container metadata %s: %w", path, err)
	}
	return &meta, nil
}

func (s *fileStore) Put(meta *ContainerMetadata) error {
	if meta == nil {
		return fmt.Errorf("put container metadata: nil metadata")
	}
	if err := util.ValidateID(meta.ID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal container metadata: %w", err)
	}
	return writeFileAtomic(s.layout.MetadataPath(meta.ID), data, 0644)
}

func (s *fileStore) Lock(id string) (func(), error) {
	if err := util.ValidateID(id); err != nil {
		return nil, err
	}
	// 잠금 파일은 번들 디렉터리 안에 있으므로 번들이 없으면(삭제됨/미존재) 잠금도 실패한다
	f, err := os.OpenFile(s.layout.LockPath(id), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("container %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock container %q: %w", id, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// writeFileAtomic : 같은 디렉터리에 임시 파일을 쓰고 fsync 후 rename하여, 쓰는 도중 중단되어도 기존 파일이 깨지지 않게 한다
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
