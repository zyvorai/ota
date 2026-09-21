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
)

func TestFetchUsesRelayBeforeSignedURL(t *testing.T) {
	payload := []byte("relay-bundle")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	var originHits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		_, _ = w.Write(payload)
	}))
	defer origin.Close()
	ou, _ := url.Parse(origin.URL)
	relayHits := 0
	relaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHits++
		if r.Header.Get("Authorization") != "Bearer relay-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/artifacts/"+digest {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer relaySrv.Close()
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("relay-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	art := Artifact{URL: origin.URL + "/os.raucb", SHA256: digest, Size: int64(len(payload))}
	d := Downloader{Config: Config{
		Backend: "simulator", StateDir: filepath.Join(dir, "state"),
		RelayURL: relaySrv.URL, RelayTokenFile: token,
		MaxArtifactBytes: 1 << 20, DownloadHosts: []string{ou.Host},
	}}
	if _, err := d.Fetch(context.Background(), art); err != nil {
		t.Fatal(err)
	}
	if relayHits != 1 || originHits != 0 {
		t.Fatalf("relay %d origin %d", relayHits, originHits)
	}

	os.Remove(filepath.Join(d.Config.StateDir, "cache", digest+".raucb"))
	relaySrv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHits++
		http.NotFound(w, r)
	})
	if _, err := d.Fetch(context.Background(), art); err != nil {
		t.Fatal(err)
	}
	if originHits != 1 {
		t.Fatalf("origin hits %d", originHits)
	}
}

func TestRelayDigestMismatchDoesNotUseOrigin(t *testing.T) {
	payload := []byte("good-bytes")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	var originHits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		_, _ = w.Write(payload)
	}))
	defer origin.Close()
	ou, _ := url.Parse(origin.URL)
	relaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("bad-bytes!"))
	}))
	defer relaySrv.Close()
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("relay-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d := Downloader{Config: Config{
		Backend: "simulator", StateDir: filepath.Join(dir, "state"),
		RelayURL: relaySrv.URL, RelayTokenFile: token,
		MaxArtifactBytes: 1 << 20, DownloadHosts: []string{ou.Host},
	}}
	art := Artifact{URL: origin.URL + "/os.raucb", SHA256: digest, Size: int64(len(payload))}
	if _, err := d.Fetch(context.Background(), art); err == nil {
		t.Fatal("accepted mismatched relay body")
	}
	if originHits != 0 {
		t.Fatal(originHits)
	}
}
