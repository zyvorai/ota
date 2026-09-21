// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCampaignExportImportKeepsEveryArtifact(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	osb := []byte("os-bytes")
	cfg := []byte("cfg-bytes")
	osDigest := digestOf(osb)
	cfgDigest := digestOf(cfg)
	rel := Release{
		Schema: 2, ID: "rel-2", Sequence: 1, Compatible: "board", Backend: "simulator",
		Version: "1.0.0", Expires: time.Now().Add(time.Hour).UTC(),
		Targets: []Target{
			{ID: "os", Type: "os.rauc", Reboot: "required", Artifact: Artifact{URL: "https://downloads.example/os.raucb", SHA256: osDigest, Size: int64(len(osb))}},
			{ID: "cfg", Type: "config.bundle", Reboot: "none", Artifact: Artifact{URL: "https://downloads.example/cfg.bin", SHA256: cfgDigest, Size: int64(len(cfg))}},
		},
	}
	env, err := SignRelease(rel, "test", priv)
	if err != nil {
		t.Fatal(err)
	}
	asg := Assignment{
		JobID: "job-1", DeviceID: "dev-1", Release: env,
		NotBefore: time.Now().Add(-time.Minute).UTC(), Deadline: time.Now().Add(time.Hour).UTC(),
	}
	src := t.TempDir()
	if err = os.WriteFile(filepath.Join(src, osDigest+".raucb"), osb, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(src, cfgDigest+".raucb"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "campaign")
	if err = ExportCampaign(out, asg, src); err != nil {
		t.Fatal(err)
	}
	got, err := ImportCampaign(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.JobID != asg.JobID || string(got.Release.Payload) != string(asg.Release.Payload) {
		t.Fatal("assignment payload changed")
	}
	media := filepath.Join(t.TempDir(), "media")
	if err = InstallCampaign(media, out, got); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(out, cfgDigest+".raucb"), []byte("tampered!!"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ImportCampaign(out); err == nil {
		t.Fatal("tampered campaign imported")
	}
}

func TestFetchPrefersMatchingLocalMedia(t *testing.T) {
	payload := []byte("bundle-bytes")
	digest := digestOf(payload)
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	if err := os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, digest+".raucb"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	d := Downloader{Config: Config{
		Backend: "simulator", StateDir: filepath.Join(dir, "state"), LocalMediaDir: media,
		MaxArtifactBytes: 1 << 20, DownloadHosts: []string{"downloads.example"},
	}}
	art := Artifact{URL: srv.URL + "/os.raucb", SHA256: digest, Size: int64(len(payload))}
	path, err := d.Fetch(context.Background(), art)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("upstream fetches: %d", hits)
	}
	if err = CheckArtifact(path, art); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(media, digest+".raucb"), []byte("wrong-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Fetch(context.Background(), art); err == nil {
		t.Fatal("mismatched local media was accepted")
	}
	if hits != 0 {
		t.Fatal("mismatch fell through to the network")
	}
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
