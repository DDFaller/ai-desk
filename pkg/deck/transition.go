package deck

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockTransition serializes process replacement across ai-desk processes.
func (s *StateDB) lockTransition(id string) (func(), error) {
	dir := filepath.Join(filepath.Dir(s.path), ".ai-desk-locks")
	if s.path == "" {
		dir = filepath.Join(os.TempDir(), "ai-desk-test-locks")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(id))
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%x.lock", sum[:16])), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
