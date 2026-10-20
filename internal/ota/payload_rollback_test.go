// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A job that fails before payloads are applied (here: missing signed SBOM) must not delete the committed
// payloads of an earlier release.
func TestFailedJobKeepsCommittedPayload(t *testing.T) {
	h := newHarness(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("config-bytes"))
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	h.e.Config.DownloadHosts = append(h.e.Config.DownloadHosts, u.Host)
	h.e.Config.RequireSBOMSignature = true
	h.e.Downloader.Config = h.e.Config
	sum := sha256.Sum256([]byte("config-bytes"))
	release := Release{
		Schema: 2, ID: "rel-2", Sequence: 1, Compatible: h.e.Config.Compatible, Backend: "simulator",
		Version: "2026.10.0", Expires: h.now.Add(time.Hour),
		Targets: []Target{{ID: "cfg", Type: "config.bundle", Reboot: "none", Rollback: "previous-digest",
			Artifact: Artifact{URL: server.URL + "/c.bin", SHA256: hex.EncodeToString(sum[:]), Size: 12}}},
	}
	env, err := SignRelease(release, "test", h.key)
	if err != nil {
		t.Fatal(err)
	}
	h.assignment.Release = env
	dir := filepath.Join(h.e.Config.StateDir, "payloads", "cfg")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "current"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	h.submit(t)
	for i := 0; i < 20 && h.state() != Failed; i++ {
		_ = h.e.Step(context.Background())
	}
	if h.state() != Failed {
		t.Fatalf("state=%s", h.state())
	}
	if got := readFile(t, filepath.Join(dir, "current")); got != "old" {
		t.Fatalf("current=%q", got)
	}
}

// A partial apply restores only what it replaced and leaves later targets alone.
func TestPartialApplyRestoresOnlyTouchedTargets(t *testing.T) {
	state := t.TempDir()
	mk := func(id, content, old string) Target {
		sum := sha256.Sum256([]byte(content))
		d := hex.EncodeToString(sum[:])
		if err := os.MkdirAll(filepath.Join(state, "cache"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "cache", d+".raucb"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(state, "payloads", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "current"), []byte(old), 0600); err != nil {
			t.Fatal(err)
		}
		return Target{ID: id, Type: "config.bundle", Artifact: Artifact{SHA256: d, Size: int64(len(content))}}
	}
	a := mk("a", "new-a", "old-a")
	a.Health = "missing-check" // fails after a is replaced
	b := mk("b", "new-b", "old-b")
	b.DependsOn = []string{"a"}
	rel := Release{Targets: []Target{a, b}}
	if err := applySidePayloads(state, rel, nil); err == nil {
		t.Fatal("expected failure")
	}
	if got := readFile(t, filepath.Join(state, "payloads", "a", "current")); got != "old-a" {
		t.Fatalf("a=%q", got)
	}
	if got := readFile(t, filepath.Join(state, "payloads", "b", "current")); got != "old-b" {
		t.Fatalf("b=%q", got)
	}
}
