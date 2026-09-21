// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyJSONMigratesAndCorruptSQLiteFailsClosed(t *testing.T) {
	dir := t.TempDir()
	legacy := Database{Schema: 1, HighSequence: 7, Jobs: map[string]Job{}, Events: []Event{}}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.View().HighSequence != 7 {
		t.Fatal("legacy journal not migrated")
	}
	s.Close()
	if _, err = os.Stat(filepath.Join(dir, "state.json.migrated")); err != nil {
		t.Fatal("legacy file not retired")
	}
	if err = os.WriteFile(filepath.Join(dir, "ota.db"), []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "ota.db-wal"))
	os.Remove(filepath.Join(dir, "ota.db-shm"))
	if _, err = OpenStore(dir); err == nil {
		t.Fatal("corrupt sqlite snapshot was accepted")
	}
}

func TestTerminalJobsArchiveInsteadOfBricking(t *testing.T) {
	prev := hotJobLimit
	hotJobLimit = 2
	t.Cleanup(func() { hotJobLimit = prev })
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 1; i <= 3; i++ {
		id := string(rune('a' + i - 1))
		job := Job{Assignment: Assignment{JobID: id, DeviceID: "device-1"}, State: Committed, Updated: time.Unix(int64(i), 0)}
		job.Fingerprint = Fingerprint(job.Assignment)
		err = s.Update(func(d *Database) error {
			if d.Jobs == nil {
				d.Jobs = map[string]Job{}
			}
			d.Jobs[id] = job
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(s.View().Jobs) != 2 {
		t.Fatalf("hot jobs=%d", len(s.View().Jobs))
	}
	var buf bytes.Buffer
	if err = s.ExportArchive(&buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"job_id"`)) {
		t.Fatalf("archive %s", buf.String())
	}
	backup := filepath.Join(dir, "backup.db")
	if err = s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(backup); err != nil {
		t.Fatal(err)
	}
}

func TestSchema2PayloadCommitsTogether(t *testing.T) {
	h := newHarness(t)
	payload := []byte("config-bytes")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	h.e.Config.DownloadHosts = append(h.e.Config.DownloadHosts, u.Host)
	h.e.Config.Capabilities = []string{"gpu.nvidia"}
	h.e.Downloader.Config = h.e.Config
	release := Release{
		Schema: 2, ID: "rel-2", Sequence: 1, Compatible: h.e.Config.Compatible, Backend: "simulator",
		Version: "2026.10.0", Expires: h.now.Add(time.Hour),
		Targets: []Target{{
			ID: "cfg", Type: "config.bundle", Reboot: "none", Rollback: "previous-digest",
			Requires: "gpu.nvidia",
			Artifact: Artifact{URL: server.URL + "/c.bin", SHA256: digest, Size: int64(len(payload))},
		}},
	}
	env, err := SignRelease(release, "test", h.key)
	if err != nil {
		t.Fatal(err)
	}
	h.assignment.Release = env
	h.submit(t)
	h.until(t, Committed)
	if _, err = os.Stat(filepath.Join(h.e.Config.StateDir, "payloads", "cfg", "current")); err != nil {
		t.Fatal(err)
	}
}
