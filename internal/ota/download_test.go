// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func downloadFixture(t *testing.T, handler http.HandlerFunc) (Downloader, Artifact) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	sum := sha256.Sum256([]byte("abcdefghij"))
	return Downloader{Config: Config{StateDir: t.TempDir(), Backend: "simulator", DownloadHosts: []string{u.Host}, MaxArtifactBytes: 1000}}, Artifact{URL: server.URL, SHA256: hex.EncodeToString(sum[:]), Size: 10}
}
func seedPartial(t *testing.T, d Downloader, a Artifact) {
	t.Helper()
	dir := filepath.Join(d.Config.StateDir, "cache")
	_ = os.MkdirAll(dir, 0700)
	if err := os.WriteFile(filepath.Join(dir, a.SHA256+".raucb.part"), []byte("abcd"), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDownloadResumesAndVerifies(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=4-" {
			t.Error(r.Header)
		}
		w.Header().Set("Content-Range", "bytes 4-9/10")
		w.WriteHeader(206)
		fmt.Fprint(w, "efghij")
	})
	seedPartial(t, d, a)
	p, e := d.Fetch(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	if e = CheckArtifact(p, a); e != nil {
		t.Fatal(e)
	}
}
func TestServerIgnoringRangeRestartsDownload(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "abcdefghij") })
	seedPartial(t, d, a)
	if _, e := d.Fetch(context.Background(), a); e != nil {
		t.Fatal(e)
	}
}
func TestDownloadRejectsMalformedAndCorruptBodies(t *testing.T) {
	for _, kind := range []string{"range", "corrupt", "oversize", "short", "encoding", "status"} {
		t.Run(kind, func(t *testing.T) {
			d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "range":
					w.Header().Set("Content-Range", "bytes 0-9/10")
					w.WriteHeader(206)
					fmt.Fprint(w, "abcdefghij")
				case "corrupt":
					fmt.Fprint(w, "0123456789")
				case "oversize":
					fmt.Fprint(w, strings.Repeat("a", 11))
				case "short":
					fmt.Fprint(w, "a")
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
					fmt.Fprint(w, "abcdefghij")
				case "status":
					w.WriteHeader(500)
				}
			})
			if kind == "range" {
				seedPartial(t, d, a)
			}
			if _, e := d.Fetch(context.Background(), a); e == nil {
				t.Fatal("bad body accepted")
			}
		})
	}
}
func TestRedirectToUntrustedHostRejected(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://untrusted.example/payload", 302)
	})
	if _, e := d.Fetch(context.Background(), a); e == nil {
		t.Fatal("redirect accepted")
	}
}
func TestProductionRejectsHTTP(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	d.Config.Backend = "rauc"
	if _, e := d.Fetch(context.Background(), a); e == nil {
		t.Fatal("HTTP accepted")
	}
}
func TestCachedArtifactAvoidsNetwork(t *testing.T) {
	calls := 0
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, "abcdefghij") })
	if _, e := d.Fetch(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Fetch(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestDiskReserveEnforced(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	d.Config.ReserveBytes = 1 << 62
	if _, e := d.Fetch(context.Background(), a); e == nil {
		t.Fatal("disk reserve ignored")
	}
}
func TestPartialSymlinkRejected(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	dir := filepath.Join(d.Config.StateDir, "cache")
	_ = os.MkdirAll(dir, 0700)
	target := filepath.Join(d.Config.StateDir, "important")
	_ = os.WriteFile(target, []byte("preserve"), 0600)
	_ = os.Symlink(target, filepath.Join(dir, a.SHA256+".raucb.part"))
	if _, e := d.Fetch(context.Background(), a); e == nil {
		t.Fatal("symlink followed")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "preserve" {
		t.Fatal("target modified")
	}
}
func TestCanceledDownload(t *testing.T) {
	d, a := downloadFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := d.Fetch(ctx, a); e == nil {
		t.Fatal("canceled request succeeded")
	}
}
