// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSBOMDigestMustMatchPinnedArtifact(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	if err := checkSBOM(Release{SBOMSHA256: digest}, 1<<20); err == nil {
		t.Fatal("pinned digest without an SBOM artifact")
	}
	mismatch := Release{SBOMSHA256: digest, SBOM: Artifact{URL: "https://downloads.example/sbom.json", SHA256: other, Size: 4}}
	if err := checkSBOM(mismatch, 1<<20); err == nil {
		t.Fatal("mismatched SBOM digest")
	}
	match := Release{SBOMSHA256: digest, SBOM: Artifact{URL: "https://downloads.example/sbom.json", SHA256: digest, Size: 4}}
	if err := checkSBOM(match, 1<<20); err != nil {
		t.Fatal(err)
	}
	arts, err := releaseArtifacts(match)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].SHA256 != digest {
		t.Fatalf("artifacts %+v", arts)
	}
}

func TestPinnedSBOMIsCheckedBeforeCommit(t *testing.T) {
	osb := []byte("signed test OS bytes\n")
	sbom := []byte("spdx-test\n")
	osSum := sha256.Sum256(osb)
	sbomSum := sha256.Sum256(sbom)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sbom") {
			_, _ = w.Write(sbom)
			return
		}
		_, _ = w.Write(osb)
	}))
	defer srv.Close()
	h := newHarness(t)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	h.e.Config.DownloadHosts = []string{u.Host}
	h.e.Downloader.Config = h.e.Config
	rel := Release{
		Schema: 1, ID: "release-sbom", Sequence: 2, Compatible: h.e.Config.Compatible, Backend: h.e.Config.Backend,
		Version: "1.0.2", Expires: h.now.Add(time.Hour),
		SBOMSHA256: hex.EncodeToString(sbomSum[:]),
		SBOM:       Artifact{URL: srv.URL + "/image.sbom", SHA256: hex.EncodeToString(sbomSum[:]), Size: int64(len(sbom))},
		Artifact:   Artifact{URL: srv.URL + "/os.raucb", SHA256: hex.EncodeToString(osSum[:]), Size: int64(len(osb))},
	}
	env, err := SignRelease(rel, "test", h.key)
	if err != nil {
		t.Fatal(err)
	}
	h.assignment = Assignment{JobID: "job-sbom", DeviceID: h.e.Config.DeviceID, Release: env, NotBefore: h.now.Add(-time.Minute), Deadline: h.now.Add(30 * time.Minute), AutoReboot: true}
	h.submit(t)
	h.until(t, Committed)
	if err = CheckArtifact(filepath.Join(h.e.Config.StateDir, "cache", rel.SBOM.SHA256+".raucb"), rel.SBOM); err != nil {
		t.Fatal(err)
	}
}

func TestSBOMByteMismatchDoesNotInstall(t *testing.T) {
	osb := []byte("signed test OS bytes\n")
	osSum := sha256.Sum256(osb)
	claimed := sha256.Sum256([]byte("different sbom"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(osb)
	}))
	defer srv.Close()
	h := newHarness(t)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	h.e.Config.DownloadHosts = []string{u.Host}
	h.e.Downloader.Config = h.e.Config
	rel := Release{
		Schema: 1, ID: "release-sbom-bad", Sequence: 3, Compatible: h.e.Config.Compatible, Backend: h.e.Config.Backend,
		Version: "1.0.3", Expires: h.now.Add(time.Hour),
		SBOMSHA256: hex.EncodeToString(claimed[:]),
		SBOM:       Artifact{URL: srv.URL + "/image.sbom", SHA256: hex.EncodeToString(claimed[:]), Size: int64(len(osb))},
		Artifact:   Artifact{URL: srv.URL + "/os.raucb", SHA256: hex.EncodeToString(osSum[:]), Size: int64(len(osb))},
	}
	env, err := SignRelease(rel, "test", h.key)
	if err != nil {
		t.Fatal(err)
	}
	h.assignment = Assignment{JobID: "job-sbom-bad", DeviceID: h.e.Config.DeviceID, Release: env, NotBefore: h.now.Add(-time.Minute), Deadline: h.now.Add(30 * time.Minute), AutoReboot: true}
	h.submit(t)
	h.step(t)
	if err = h.e.Step(context.Background()); err == nil {
		t.Fatal("installed an SBOM whose bytes do not match the pinned digest")
	}
	if h.state() == Installing || h.state() == Committed || h.state() == AwaitingReboot {
		t.Fatal(h.state())
	}
}
