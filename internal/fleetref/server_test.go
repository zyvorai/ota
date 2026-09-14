// SPDX-License-Identifier: Apache-2.0
package fleetref_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyvorai/ota/internal/fleetref"
	"github.com/zyvorai/ota/internal/ota"
)

func TestRefServerAssignmentAndEventAck(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("lab-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "healthy"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := ota.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := ota.Config{
		DeviceID: "minewing-gw1-lab-001", Compatible: "minewing-gw1-r1",
		StateDir: dir, Socket: filepath.Join(dir, "agent.sock"),
		Backend: "simulator", TrustKeys: map[string]string{"production-1": base64.StdEncoding.EncodeToString(pub)},
		DownloadHosts: []string{"127.0.0.1"}, MaxArtifactBytes: 1 << 20, ReserveBytes: 1 << 20,
		HealthTimeoutSeconds: 10, HealthStableSeconds: 0,
		Checks:         []ota.Check{{Kind: "file", Target: filepath.Join(dir, "healthy")}},
		FleetTokenFile: tokenPath,
	}
	sim, err := ota.NewSimulator(dir, cfg.Compatible)
	if err != nil {
		t.Fatal(err)
	}
	engine := ota.NewEngine(cfg, store, sim)

	now := time.Now().UTC()
	release := ota.Release{
		Schema: 1, ID: "rel-1", Sequence: 1, Compatible: cfg.Compatible, Backend: "simulator",
		Version: "0.1.1", Expires: now.Add(time.Hour),
		Artifact: ota.Artifact{URL: "http://127.0.0.1/os.raucb", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1},
	}
	env, err := ota.SignRelease(release, "production-1", priv)
	if err != nil {
		t.Fatal(err)
	}
	assignment := ota.Assignment{
		JobID: "fleet-job-1", DeviceID: cfg.DeviceID, Release: env,
		NotBefore: now.Add(-time.Minute), Deadline: now.Add(30 * time.Minute), AutoReboot: true,
	}

	ref := fleetref.New()
	if err := ref.RegisterDevice(cfg.DeviceID, "lab-token"); err != nil {
		t.Fatal(err)
	}
	ref.SetAssignment(cfg.DeviceID, &assignment)

	srv := httptest.NewTLSServer(ref.Handler())
	t.Cleanup(srv.Close)
	cfg.FleetURL = srv.URL
	fleet, err := ota.NewFleet(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fleet.Client = srv.Client()

	if err := fleet.Sync(context.Background(), engine); err != nil {
		t.Fatal(err)
	}
	if engine.Store.View().Active != "fleet-job-1" {
		t.Fatalf("assignment not accepted: %+v", engine.Store.View())
	}
	if len(engine.Store.View().Events) == 0 {
		t.Fatal("expected outbox events")
	}
	ref.SetAssignment(cfg.DeviceID, nil)
	if err := fleet.Sync(context.Background(), engine); err != nil {
		t.Fatal(err)
	}
	if len(engine.Store.View().Events) != 0 {
		t.Fatalf("events not acked: %v", engine.Store.View().Events)
	}
	if ref.AckedSequence(cfg.DeviceID) == 0 {
		t.Fatal("server ack sequence not advanced")
	}
}

func TestRefServerRejectsUnknownDevice(t *testing.T) {
	ref := fleetref.New()
	_ = ref.RegisterDevice("known", "tok")
	srv := httptest.NewTLSServer(ref.Handler())
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/devices/unknown/assignment", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestRefServerIdempotentEvents(t *testing.T) {
	ref := fleetref.New()
	_ = ref.RegisterDevice("dev-1", "tok")
	srv := httptest.NewTLSServer(ref.Handler())
	t.Cleanup(srv.Close)

	ev := []ota.Event{{Sequence: 1, JobID: "j", State: ota.Accepted, Time: time.Now().UTC()}}
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	post := func() *http.Response {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/devices/dev-1/events", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer tok")
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	r1 := post()
	defer r1.Body.Close()
	r2 := post()
	defer r2.Body.Close()
	if r1.StatusCode != 200 || r2.StatusCode != 200 {
		t.Fatalf("status %d %d", r1.StatusCode, r2.StatusCode)
	}
	b, _ := io.ReadAll(r2.Body)
	var ack struct {
		Sequence uint64 `json:"sequence"`
	}
	if err := json.Unmarshal(b, &ack); err != nil || ack.Sequence != 1 {
		t.Fatalf("ack=%s err=%v", b, err)
	}
	if ref.AckedSequence("dev-1") != 1 {
		t.Fatal(ref.AckedSequence("dev-1"))
	}
}
