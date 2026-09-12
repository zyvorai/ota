// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Store uses a single-writer, fsync+rename snapshot journal. StateDir must reside
// on a persistent local filesystem shared by both OS slots, never tmpfs/NFS.
type Store struct {
	mu       sync.Mutex
	path     string
	lock     *os.File
	data     Database
	poisoned bool
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return nil, errors.New("state directory must not be symlink or group/world writable")
	}
	f, err := os.OpenFile(filepath.Join(dir, "agent.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another agent owns state directory")
	}
	s := &Store{path: filepath.Join(dir, "state.json"), lock: f, data: Database{Schema: 1, Jobs: map[string]Job{}}}
	b, err := os.ReadFile(s.path)
	if err == nil {
		err = StrictJSON(b, &s.data)
		if err == nil {
			err = validateDatabase(s.data)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		s.Close()
		return nil, err
	}
	return s, nil
}
func validateDatabase(d Database) error {
	if d.Schema != 1 || d.Jobs == nil || len(d.Jobs) > 10000 || len(d.Events) > 10000 {
		return errors.New("unsupported or corrupt state")
	}
	for id, j := range d.Jobs {
		if id != j.Assignment.JobID || j.Fingerprint != Fingerprint(j.Assignment) || j.Release.Sequence > d.HighSequence {
			return errors.New("journal job integrity failure")
		}
		switch j.State {
		case Accepted, Downloading, Verified, Installing, AwaitingReboot, CheckingHealth, Committing, Committed, RollbackPending, RolledBack, Failed, NeedsRecovery:
		default:
			return errors.New("unknown journal job state")
		}
		if !Terminal(j.State) && d.Active != id {
			return errors.New("orphaned active journal job")
		}
	}
	if d.Active != "" {
		j, ok := d.Jobs[d.Active]
		if !ok || Terminal(j.State) {
			return errors.New("invalid active job reference")
		}
	}
	var previous uint64
	for _, event := range d.Events {
		if event.Sequence <= previous || event.Sequence > d.EventSequence {
			return errors.New("invalid event ordering")
		}
		previous = event.Sequence
	}
	return nil
}
func (s *Store) Close() error { return s.lock.Close() }
func cloneDB(d Database) Database {
	b, _ := json.Marshal(d)
	var c Database
	_ = json.Unmarshal(b, &c)
	return c
}
func (s *Store) View() Database { s.mu.Lock(); defer s.mu.Unlock(); return cloneDB(s.data) }
func (s *Store) Update(fn func(*Database) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poisoned {
		return errors.New("state write failed; restart and inspect storage before continuing")
	}
	next := cloneDB(s.data)
	if err := fn(&next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = AtomicWrite(s.path, b, 0600); err != nil {
		s.poisoned = true
		return fmt.Errorf("persist state: %w", err)
	}
	s.data = next
	return nil
}
func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".ota-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
