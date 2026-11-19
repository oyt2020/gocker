package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oyt2020/gocker/pkg/util"
)

// ErrNotFound 컨테이너 레코드가 존재하지 않음
var ErrNotFound = errors.New("container not found")

const (
	recordExt = ".json"
	lockExt   = ".lock"
	// namesLockFile 저장소 전역 잠금 파일 이름. 컨테이너 ID 형식이 아니므로 레코드/컨테이너 잠금 파일과 겹치지 않는다.
	namesLockFile = "names" + lockExt
)

// ContainerStore 컨테이너 메타데이터 저장소.
//
// 컨테이너의 "존재"는 이 저장소의 레코드로 정의된다: 레코드가 있으면 컨테이너가 있는 것이고,
// 번들 디렉터리나 runc 상태는 그 레코드에 딸린 리소스다.
type ContainerStore interface {
	// Get : 저장된 메타데이터를 읽는다 (부작용 없음). 없으면 ErrNotFound.
	Get(id string) (*ContainerMetadata, error)
	// Put : 메타데이터를 원자적으로 저장한다 (임시 파일 작성 후 rename)
	Put(meta *ContainerMetadata) error
	// Delete : 레코드를 삭제한다. 이미 없으면 nil (멱등).
	Delete(id string) error
	// List : 저장된 모든 메타데이터를 ID 순으로 반환한다
	List() ([]*ContainerMetadata, error)
	// Lock : 컨테이너 단위 배타 잠금(flock)을 획득한다. 다른 gocker 프로세스와의 동시 상태 변경을 직렬화한다.
	// 레코드가 아직 없는 ID도 잠글 수 있다 (생성 과정 전체를 보호하기 위함).
	Lock(id string) (unlock func(), err error)
	// LockNames : 저장소 전역 배타 잠금(flock)을 획득한다. 컨테이너 이름처럼 여러 레코드에 걸친 유일성 제약을
	// "검사 → 기록"할 때, 그 사이에 다른 gocker 프로세스가 끼어들지 못하도록 묶는 용도다.
	// 컨테이너 잠금과 함께 쓸 때는 항상 컨테이너 잠금을 먼저 잡는다 (교착 방지).
	LockNames() (unlock func(), err error)
}

// fileStore : 디렉터리 하나에 <id>.json(레코드)과 <id>.lock(잠금 파일)을 두는 구현
type fileStore struct {
	dir string
}

// NewFileStore : dir 아래에 메타데이터를 저장하는 ContainerStore 생성. 번들 디렉터리와는 독립적이다.
func NewFileStore(dir string) ContainerStore {
	return &fileStore{dir: dir}
}

func (s *fileStore) recordPath(id string) string {
	return filepath.Join(s.dir, id+recordExt)
}

func (s *fileStore) lockPath(id string) string {
	return filepath.Join(s.dir, id+lockExt)
}

func (s *fileStore) Get(id string) (*ContainerMetadata, error) {
	if err := util.ValidateID(id); err != nil {
		return nil, err
	}
	return s.read(id)
}

func (s *fileStore) read(id string) (*ContainerMetadata, error) {
	path := s.recordPath(id)
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
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}
	return writeFileAtomic(s.recordPath(meta.ID), data, 0644)
}

func (s *fileStore) Delete(id string) error {
	if err := util.ValidateID(id); err != nil {
		return err
	}
	if err := os.Remove(s.recordPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete container metadata %q: %w", id, err)
	}
	return nil
}

func (s *fileStore) List() ([]*ContainerMetadata, error) {
	entries, err := os.ReadDir(s.dir) // 이름순 정렬
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read store dir: %w", err)
	}

	var list []*ContainerMetadata
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), recordExt)
		if !ok || entry.IsDir() || !util.IsValidID(id) {
			continue // 잠금 파일, 원자적 쓰기의 임시 파일 등
		}
		meta, err := s.read(id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // 목록을 읽는 사이 삭제됨
			}
			return nil, err
		}
		list = append(list, meta)
	}
	return list, nil
}

func (s *fileStore) Lock(id string) (func(), error) {
	if err := util.ValidateID(id); err != nil {
		return nil, err
	}
	f, err := s.acquire(s.lockPath(id))
	if err != nil {
		return nil, fmt.Errorf("lock container %q: %w", id, err)
	}
	return func() { s.unlock(id, f) }, nil
}

func (s *fileStore) LockNames() (func(), error) {
	path := filepath.Join(s.dir, namesLockFile)
	f, err := s.acquire(path)
	if err != nil {
		return nil, fmt.Errorf("lock container names: %w", err)
	}
	return func() {
		// 잠금 파일을 남기지 않는다. 잠금을 쥔 상태에서 지우므로, 대기 중이던 쪽은 acquire의 재확인에서 새 파일로 재시도한다.
		_ = os.Remove(path)
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// acquire : path의 잠금 파일에 배타 flock을 잡고 그 파일을 반환한다 (해제는 호출자 책임).
func (s *fileStore) acquire(path string) (*os.File, error) {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("open lock file: %w", err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flock: %w", err)
		}

		// 잠금을 기다리는 사이 이전 보유자가 잠금 파일을 지웠다면(unlock / LockNames 참고) 지금 잡은 것은
		// 이미 경로에서 사라진 파일의 잠금이다. 경로가 여전히 같은 파일을 가리킬 때만 유효하다.
		held, err1 := f.Stat()
		current, err2 := os.Stat(path)
		if err1 == nil && err2 == nil && os.SameFile(held, current) {
			return f, nil
		}
		_ = f.Close() // close가 flock도 해제한다 → 새 파일로 재시도
	}
}

// unlock : 잠금 해제. 레코드가 없는 ID(삭제 완료 / 생성 실패 / 존재하지 않는 ID 조회)의 잠금 파일은
// 남겨 둘 이유가 없으므로 잠금을 쥔 상태에서 지운다. 대기 중이던 쪽은 Lock의 재확인에서 새 파일로 재시도한다.
func (s *fileStore) unlock(id string, f *os.File) {
	if _, err := os.Stat(s.recordPath(id)); errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(s.lockPath(id))
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
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
