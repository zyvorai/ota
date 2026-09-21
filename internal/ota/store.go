// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// Hot limits. Terminal jobs above hotJobLimit are archived in the same
// transaction. Unacknowledged events are never dropped to make room.
var (
	hotJobLimit   = 10000
	hotEventLimit = 9000
)

// Store is a single-writer SQLite journal (WAL, full synchronous). StateDir must
// reside on a persistent local filesystem shared by both OS slots, never tmpfs/NFS.
type Store struct {
	mu           sync.Mutex
	dir          string
	lock         *os.File
	db           *sql.DB
	data         Database
	poisoned     bool
	beforeCommit func()
	afterCommit  func()
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
	dbPath := filepath.Join(dir, "ota.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		f.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{dir: dir, lock: f, db: db, data: Database{Schema: 1, Jobs: map[string]Job{}}}
	if err = s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	if err = s.load(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS snapshot (id INTEGER PRIMARY KEY CHECK (id = 1), body BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS archive_jobs (job_id TEXT PRIMARY KEY, body BLOB NOT NULL, archived_at TEXT NOT NULL);
INSERT INTO schema_migrations(version) VALUES (1) ON CONFLICT(version) DO NOTHING;
`)
	return err
}

func (s *Store) load() error {
	legacyPath := filepath.Join(s.dir, "state.json")
	legacy, legacyErr := os.ReadFile(legacyPath)
	var legacyDB Database
	hasLegacy := legacyErr == nil
	if hasLegacy {
		if err := StrictJSON(legacy, &legacyDB); err != nil {
			return fmt.Errorf("corrupt state.json: %w", err)
		}
		if err := validateDatabase(legacyDB); err != nil {
			return err
		}
	} else if legacyErr != nil && !os.IsNotExist(legacyErr) {
		return legacyErr
	}
	var body []byte
	err := s.db.QueryRow(`SELECT body FROM snapshot WHERE id = 1`).Scan(&body)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if hasLegacy {
			if err = s.persist(legacyDB, nil); err != nil {
				return err
			}
			s.data = legacyDB
			return os.Rename(legacyPath, legacyPath+".migrated")
		}
		return nil
	case err != nil:
		return fmt.Errorf("sqlite snapshot: %w", err)
	}
	if err = StrictJSON(body, &s.data); err != nil {
		return fmt.Errorf("corrupt sqlite snapshot: %w", err)
	}
	if err = validateDatabase(s.data); err != nil {
		return err
	}
	if hasLegacy {
		// A corrupt file already returned above. A valid leftover is retired so
		// the next start does not depend on two copies.
		_ = os.Rename(legacyPath, legacyPath+".migrated")
	}
	return nil
}

func validateDatabase(d Database) error {
	if d.Schema != 1 || d.Jobs == nil || len(d.Jobs) > hotJobLimit+1 || len(d.Events) > 10000 {
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

func (s *Store) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.lock != nil {
		if e := s.lock.Close(); err == nil {
			err = e
		}
	}
	return err
}

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
	spilled, err := compactHot(&next)
	if err != nil {
		return err
	}
	if err = validateDatabase(next); err != nil {
		return err
	}
	if err = s.persist(next, spilled); err != nil {
		s.poisoned = true
		return fmt.Errorf("persist state: %w", err)
	}
	s.data = next
	return nil
}

func compactHot(d *Database) ([]Job, error) {
	var spilled []Job
	for len(d.Jobs) > hotJobLimit {
		id := ""
		var oldest time.Time
		for k, j := range d.Jobs {
			if k == d.Active || !Terminal(j.State) {
				continue
			}
			if id == "" || j.Updated.Before(oldest) {
				id = k
				oldest = j.Updated
			}
		}
		if id == "" {
			return nil, errors.New("journal capacity reached; export/ack events or reprovision journal under documented procedure")
		}
		spilled = append(spilled, d.Jobs[id])
		delete(d.Jobs, id)
	}
	if len(d.Events) > 10000 {
		return nil, errors.New("event outbox full; update paused")
	}
	return spilled, nil
}

func (s *Store) persist(d Database, spilled []Job) error {
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO snapshot(id, body) VALUES(1, ?) ON CONFLICT(id) DO UPDATE SET body = excluded.body`, body); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, j := range spilled {
		b, err := json.Marshal(j)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO archive_jobs(job_id, body, archived_at) VALUES(?, ?, ?) ON CONFLICT(job_id) DO UPDATE SET body = excluded.body, archived_at = excluded.archived_at`, j.Assignment.JobID, b, now); err != nil {
			return err
		}
	}
	if s.beforeCommit != nil {
		s.beforeCommit()
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.afterCommit != nil {
		s.afterCommit()
	}
	return nil
}

// Backup writes a consistent copy. The destination must not already exist.
func (s *Store) Backup(dest string) error {
	dest = filepath.Clean(dest)
	if !filepath.IsAbs(dest) {
		return errors.New("backup destination must be absolute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(dest); err == nil {
		return errors.New("backup destination exists")
	}
	_, err := s.db.Exec(`VACUUM INTO '` + strings.ReplaceAll(dest, "'", "''") + `'`)
	return err
}

// ExportArchive writes archived jobs as a JSON array. Hot jobs are not included.
func (s *Store) ExportArchive(w io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT body FROM archive_jobs ORDER BY archived_at, job_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var jobs []json.RawMessage
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return err
		}
		jobs = append(jobs, json.RawMessage(b))
	}
	if jobs == nil {
		jobs = []json.RawMessage{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jobs)
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
