// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type harness struct {
	e          *Engine
	s          *Store
	sim        *Simulator
	key        ed25519.PrivateKey
	now        time.Time
	assignment Assignment
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	payload := []byte("signed test OS bytes\n")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	c := Config{DeviceID: "device-1", Compatible: "test-board", StateDir: dir, Socket: filepath.Join(dir, "agent.sock"), Backend: "simulator", TrustKeys: map[string]string{"test": base64.StdEncoding.EncodeToString(pub)}, DownloadHosts: []string{u.Host}, MaxArtifactBytes: 1 << 20, HealthTimeoutSeconds: 10, HealthStableSeconds: 0}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sim, err := NewSimulator(dir, c.Compatible)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{s: store, sim: sim, key: priv, now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	h.e = NewEngine(c, store, sim)
	h.e.Now = func() time.Time { return h.now }
	release := Release{Schema: 1, ID: "release-1", Sequence: 1, Compatible: c.Compatible, Backend: c.Backend, Version: "1.0.1", Expires: h.now.Add(time.Hour), Artifact: Artifact{URL: server.URL + "/os.raucb", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload))}}
	env, err := SignRelease(release, "test", priv)
	if err != nil {
		t.Fatal(err)
	}
	h.assignment = Assignment{JobID: "job-1", DeviceID: c.DeviceID, Release: env, NotBefore: h.now.Add(-time.Minute), Deadline: h.now.Add(30 * time.Minute), AutoReboot: true}
	return h
}
func (h *harness) submit(t *testing.T) {
	t.Helper()
	if _, err := h.e.Submit(h.assignment); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) state() State { return h.s.View().Jobs[h.assignment.JobID].State }
func (h *harness) step(t *testing.T) {
	t.Helper()
	if err := h.e.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) until(t *testing.T, want State) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if h.state() == want {
			return
		}
		h.step(t)
	}
	t.Fatalf("state=%s want=%s", h.state(), want)
}

func TestSuccessfulUpdateCommitsOnlyAfterHealth(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, CheckingHealth)
	before, _ := h.sim.Status(context.Background())
	if before.Slots[1].Good {
		t.Fatal("new slot prematurely marked good")
	}
	h.until(t, Committed)
	s, _ := h.sim.Status(context.Background())
	if s.Booted != "rootfs.1" || !s.Slots[1].Good {
		t.Fatalf("bad result: %+v", s)
	}
	if h.s.View().Active != "" {
		t.Fatal("active job retained")
	}
	if len(h.s.View().Events) != 9 {
		t.Fatalf("events=%d", len(h.s.View().Events))
	}
}
func TestHealthFailureRollsBack(t *testing.T) {
	h := newHarness(t)
	h.e.Health = func(context.Context) error { return errors.New("device unhealthy") }
	h.submit(t)
	h.until(t, CheckingHealth)
	h.step(t)
	h.now = h.now.Add(11 * time.Second)
	h.until(t, RolledBack)
	s, _ := h.sim.Status(context.Background())
	if s.Booted != "rootfs.0" || s.Primary != "rootfs.0" {
		t.Fatal(s)
	}
}
func TestUnbootableUpdateFallsBack(t *testing.T) {
	h := newHarness(t)
	h.sim.FailBoot = true
	h.submit(t)
	h.until(t, RolledBack)
	s, _ := h.sim.Status(context.Background())
	if s.Booted != "rootfs.0" {
		t.Fatal(s)
	}
}
func TestRestartAfterRebootResumesHealth(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, AwaitingReboot)
	h.step(t)
	c := h.e.Config
	h.e = NewEngine(c, h.s, h.sim)
	h.e.Now = func() time.Time { return h.now }
	h.until(t, Committed)
}
func TestRestartDuringInstallRequiresRecovery(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Verified)
	j, _ := h.e.current()
	j.OldSlot = "rootfs.0"
	j.TargetSlot = "rootfs.1"
	if err := h.e.save(j, Installing, ""); err != nil {
		t.Fatal(err)
	}
	h.step(t)
	if h.state() != NeedsRecovery {
		t.Fatal(h.state())
	}
	if err := h.e.RecoverAbort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.state() != Failed {
		t.Fatal(h.state())
	}
}
func TestBackendFailureNeverRetriesInstall(t *testing.T) {
	h := newHarness(t)
	h.sim.FailInstall = true
	h.submit(t)
	h.until(t, NeedsRecovery)
	h.sim.FailInstall = false
	for i := 0; i < 5; i++ {
		h.step(t)
	}
	if h.state() != NeedsRecovery {
		t.Fatal(h.state())
	}
	s, _ := h.sim.Status(context.Background())
	if s.VersionOf("rootfs.1") != "factory" {
		t.Fatal(s)
	}
}
func TestDuplicateAssignmentAndConflictingID(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	if _, err := h.e.Submit(h.assignment); err != nil {
		t.Fatal(err)
	}
	if len(h.s.View().Events) != 1 {
		t.Fatal("duplicate event")
	}
	changed := h.assignment
	changed.AutoReboot = false
	if _, err := h.e.Submit(changed); err == nil {
		t.Fatal("conflicting ID accepted")
	}
}
func TestReplayAfterCompletedUpdateRejected(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Committed)
	a := h.assignment
	a.JobID = "replay"
	if _, err := h.e.Submit(a); err == nil {
		t.Fatal("replay accepted")
	}
}
func TestConcurrentSubmissionOneWinner(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = h.e.Submit(h.assignment) }()
	}
	wg.Wait()
	if len(h.s.View().Jobs) != 1 || len(h.s.View().Events) != 1 {
		t.Fatal(h.s.View())
	}
}
func TestMaintenanceWindowAndExpiry(t *testing.T) {
	h := newHarness(t)
	h.assignment.NotBefore = h.now.Add(time.Minute)
	h.submit(t)
	h.step(t)
	if h.state() != Accepted {
		t.Fatal("started before window")
	}
	h.now = h.now.Add(time.Hour)
	h.step(t)
	if h.state() != Failed {
		t.Fatal(h.state())
	}
}
func TestManualReboot(t *testing.T) {
	h := newHarness(t)
	h.assignment.AutoReboot = false
	h.submit(t)
	h.until(t, AwaitingReboot)
	h.step(t)
	if h.state() != AwaitingReboot {
		t.Fatal(h.state())
	}
	if err := h.e.RequestReboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.until(t, Committed)
}
func TestManualRebootOutsideWindowRejected(t *testing.T) {
	h := newHarness(t)
	h.assignment.AutoReboot = false
	h.submit(t)
	h.until(t, AwaitingReboot)
	h.now = h.now.Add(time.Hour)
	if err := h.e.RequestReboot(context.Background()); err == nil {
		t.Fatal("expired reboot allowed")
	}
}
func TestHealthStabilityWindow(t *testing.T) {
	h := newHarness(t)
	h.e.Config.HealthStableSeconds = 3
	h.submit(t)
	h.until(t, CheckingHealth)
	h.step(t)
	h.now = h.now.Add(2 * time.Second)
	h.step(t)
	if h.state() != CheckingHealth {
		t.Fatal("committed before stable")
	}
	h.now = h.now.Add(time.Second)
	h.until(t, Committed)
}
func TestRestartCommittingDoesNotReinstall(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Committing)
	h.e = NewEngine(h.e.Config, h.s, h.sim)
	h.e.Now = func() time.Time { return h.now }
	h.sim.FailInstall = true
	h.until(t, Committed)
}
func TestRebootDuringCommitRechecksHealth(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Committing)
	if err := h.sim.Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.e.Health = func(context.Context) error { return errors.New("bad") }
	h.step(t)
	if h.state() != CheckingHealth {
		t.Fatal(h.state())
	}
	h.now = h.now.Add(11 * time.Second)
	h.until(t, RolledBack)
}
func TestTamperedCacheRejectedBeforeInstall(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Verified)
	j, _ := h.e.current()
	path := filepath.Join(h.e.Config.StateDir, "cache", j.Release.Artifact.SHA256+".raucb")
	if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	h.step(t)
	if h.state() != Failed {
		t.Fatal(h.state())
	}
}
func TestEventAckAndCacheGC(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	if err := h.e.GC(); err == nil {
		t.Fatal("GC active job")
	}
	h.until(t, Committed)
	d := h.s.View()
	if err := h.e.Ack(d.EventSequence + 1); err == nil {
		t.Fatal("future ack")
	}
	if err := h.e.Ack(d.EventSequence); err != nil {
		t.Fatal(err)
	}
	if len(h.s.View().Events) != 0 {
		t.Fatal("events retained")
	}
	if err := h.e.GC(); err != nil {
		t.Fatal(err)
	}
}
func TestVerifyRejectsBadSignaturesAndMetadata(t *testing.T) {
	for _, kind := range []string{"signature", "key", "board", "backend", "expired", "digest", "size", "sequence", "unknown_field", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			env := h.assignment.Release
			var r Release
			_ = json.Unmarshal(env.Payload, &r)
			switch kind {
			case "signature":
				env.Signature[0] ^= 1
			case "key":
				env.KeyID = "stranger"
			case "board":
				r.Compatible = "different"
			case "backend":
				r.Backend = "rauc"
			case "expired":
				r.Expires = h.now
			case "digest":
				r.Artifact.SHA256 = "../../evil"
			case "size":
				r.Artifact.Size = 0
			case "sequence":
				r.Sequence = 0
			}
			if kind != "signature" && kind != "key" {
				env, _ = SignRelease(r, "test", h.key)
			}
			if kind == "unknown_field" {
				env.Payload = append(env.Payload[:len(env.Payload)-1], []byte(`,"surprise":true}`)...)
				env.Signature = ed25519.Sign(h.key, env.Payload)
			}
			if kind == "trailing" {
				env.Payload = append(env.Payload, []byte(` {}`)...)
				env.Signature = ed25519.Sign(h.key, env.Payload)
			}
			if _, err := VerifyEnvelope(env, h.e.Config, h.now); err == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
}
func TestStorePersistenceLockAndCorruption(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = OpenStore(dir); err == nil {
		t.Fatal("second owner allowed")
	}
	if err = s.Update(func(d *Database) error { d.HighSequence = 42; return nil }); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.View().HighSequence != 42 {
		t.Fatal("lost durable state")
	}
	s.Close()
	_ = os.WriteFile(filepath.Join(dir, "state.json"), []byte("{"), 0600)
	if _, err = OpenStore(dir); err == nil {
		t.Fatal("corrupt store silently reset")
	}
}
func TestStoreTransactionRollback(t *testing.T) {
	h := newHarness(t)
	err := h.s.Update(func(d *Database) error { d.HighSequence = 99; return errors.New("abort") })
	if err == nil || h.s.View().HighSequence != 0 {
		t.Fatal("aborted transaction persisted")
	}
}
func TestConfigRejectsUnsafeSettings(t *testing.T) {
	h := newHarness(t)
	for _, kind := range []string{"backend", "writes", "checks", "path", "health", "fleet", "check"} {
		t.Run(kind, func(t *testing.T) {
			c := h.e.Config
			switch kind {
			case "backend":
				c.Backend = "unknown"
			case "writes":
				c.Backend = "rauc"
			case "checks":
				c.Backend = "rauc"
				c.AllowDeviceWrites = true
			case "path":
				c.StateDir = "relative"
			case "health":
				c.HealthTimeoutSeconds = 0
			case "fleet":
				c.FleetURL = "http://example.com"
			case "check":
				c.Checks = []Check{{Kind: "http", Target: "http://example.com/health"}}
			}
			if c.Validate() == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
}

func TestNormalBootIsHealthConfirmed(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Committed)
	if err := h.sim.Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.step(t)
	if h.s.View().BootCheck.Confirmed {
		t.Fatal("premature normal boot confirmation")
	}
	h.step(t)
	if !h.s.View().BootCheck.Confirmed {
		t.Fatal("normal boot not confirmed")
	}
}
func TestNormalBootHealthFailureUsesKnownFallback(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Committed)
	_ = h.sim.Reboot(context.Background())
	h.e.Health = func(context.Context) error { return errors.New("unhealthy") }
	h.step(t)
	h.now = h.now.Add(11 * time.Second)
	h.step(t)
	s, _ := h.sim.Status(context.Background())
	if s.Booted != "rootfs.0" {
		t.Fatal("normal boot did not fall back")
	}
}

func TestJournalWriteFailurePoisonsStore(t *testing.T) {
	h := newHarness(t)
	if err := h.s.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Update(func(d *Database) error { d.HighSequence = 5; return nil }); err == nil {
		t.Fatal("expected persistence failure")
	}
	if err := h.s.Update(func(d *Database) error { return nil }); err == nil {
		t.Fatal("poisoned store accepted write")
	}
	if h.s.View().HighSequence != 0 {
		t.Fatal("unpersisted state became visible")
	}
}

func TestNormalBootDoesNotBounceBackToFailedSlot(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.until(t, Committed)
	_ = h.sim.Reboot(context.Background())
	h.e.Health = func(context.Context) error { return errors.New("unhealthy") }
	h.step(t)
	h.now = h.now.Add(11 * time.Second)
	h.step(t)
	h.step(t)
	h.now = h.now.Add(11 * time.Second)
	if err := h.e.Step(context.Background()); err == nil {
		t.Fatal("expected no healthy fallback error")
	}
	s, _ := h.sim.Status(context.Background())
	if s.Booted != "rootfs.0" {
		t.Fatal("bounced into failed slot")
	}
}
