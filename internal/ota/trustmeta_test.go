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
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRootUpdateNeedsThresholdAndRefusesRollback(t *testing.T) {
	keys, pubs := testKeys(t, "a", "b", "c")
	dir := t.TempDir()
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	root := rootMeta{
		Schema: 1, Version: 1, Expires: now.Add(time.Hour), Keys: pubs,
		Roles: map[string]metaRole{"root": {Threshold: 2, KeyIDs: []string{"a", "b", "c"}}},
	}
	payload := mustJSON(t, root)
	writeMeta(t, filepath.Join(dir, "root.json"), signMeta(payload, keys, "a"))
	if _, _, err := loadRoot(dir, digest(payload), nil, now); err == nil {
		t.Fatal("one signature met a threshold of two")
	}
	writeMeta(t, filepath.Join(dir, "root.json"), signMeta(payload, keys, "a", "b"))
	_, stored, err := loadRoot(dir, digest(payload), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	root.Version = 2
	next := mustJSON(t, root)
	writeMeta(t, filepath.Join(dir, "root.json"), signMeta(next, keys, "a"))
	if _, _, err = loadRoot(dir, digest(payload), stored, now); err == nil {
		t.Fatal("root update below threshold")
	}
	writeMeta(t, filepath.Join(dir, "root.json"), signMeta(next, keys, "a", "c"))
	if _, _, err = loadRoot(dir, digest(payload), stored, now); err != nil {
		t.Fatal(err)
	}
	writeMeta(t, filepath.Join(dir, "root.json"), signMeta(payload, keys, "a", "b"))
	if _, _, err = loadRoot(dir, digest(payload), next, now); err == nil {
		t.Fatal("root rollback accepted")
	}
}

func TestDelegatedKeyCannotSignOS(t *testing.T) {
	root := rootMeta{Roles: map[string]metaRole{
		"targets": {Threshold: 1, KeyIDs: []string{"online"}},
	}, Delegations: []delegation{{Name: "site", Threshold: 1, KeyIDs: []string{"site"}, Types: []string{"config.bundle"}}}}
	if err := authorizeReleaseKey(root, "site", []string{"os.rauc"}); err == nil {
		t.Fatal("site key signed an OS release")
	}
	if err := authorizeReleaseKey(root, "site", []string{"config.bundle"}); err != nil {
		t.Fatal(err)
	}
	if err := authorizeReleaseKey(root, "online", []string{"os.rauc"}); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredTimestampAndSnapshotMismatch(t *testing.T) {
	keys, pubs := testKeys(t, "timestamp", "snapshot", "targets", "online")
	dir := t.TempDir()
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	root := rootMeta{
		Schema: 1, Version: 1, Expires: now.Add(time.Hour), Keys: pubs,
		Roles: map[string]metaRole{
			"root":      {Threshold: 1, KeyIDs: []string{"timestamp"}},
			"timestamp": {Threshold: 1, KeyIDs: []string{"timestamp"}},
			"snapshot":  {Threshold: 1, KeyIDs: []string{"snapshot"}},
			"targets":   {Threshold: 1, KeyIDs: []string{"targets", "online"}},
		},
	}
	rootPayload := mustJSON(t, root)
	writeMeta(t, filepath.Join(dir, "root.json"), signMeta(rootPayload, keys, "timestamp"))
	rel := Release{Schema: 1, ID: "rel", Sequence: 1, Compatible: "test-board", Backend: "simulator", Version: "1", Expires: now.Add(time.Hour), Artifact: Artifact{URL: "https://downloads.example/a", SHA256: stringsRepeat(), Size: 4}}
	relPayload := mustJSON(t, rel)
	targets := targetsMeta{Version: 1, Expires: now.Add(time.Hour), ReleaseSHA256: []string{digest(relPayload)}}
	targetsPayload := mustJSON(t, targets)
	writeMeta(t, filepath.Join(dir, "targets.json"), signMeta(targetsPayload, keys, "targets"))
	snap := snapshotMeta{Version: 1, Expires: now.Add(time.Hour), TargetsVersion: 1, TargetsSHA256: digest(targetsPayload)}
	snapPayload := mustJSON(t, snap)
	writeMeta(t, filepath.Join(dir, "snapshot.json"), signMeta(snapPayload, keys, "snapshot"))
	ts := timestampMeta{Version: 1, Expires: now.Add(-time.Minute), SnapshotVersion: 1, SnapshotSHA256: digest(snapPayload)}
	writeMeta(t, filepath.Join(dir, "timestamp.json"), signMeta(mustJSON(t, ts), keys, "timestamp"))
	c := Config{TrustDir: dir, RootSHA256: digest(rootPayload)}
	if _, err := evaluateSupplyChain(c, Database{}, relPayload, "online", rel, now); err == nil {
		t.Fatal("expired timestamp accepted")
	}
	ts.Expires = now.Add(time.Hour)
	ts.SnapshotSHA256 = stringsRepeat()
	writeMeta(t, filepath.Join(dir, "timestamp.json"), signMeta(mustJSON(t, ts), keys, "timestamp"))
	if _, err := evaluateSupplyChain(c, Database{}, relPayload, "online", rel, now); err == nil {
		t.Fatal("snapshot mismatch accepted")
	}
}

func TestRejectedDigestAndTransparencyChain(t *testing.T) {
	dir := t.TempDir()
	if err := appendTransparency(dir, "rel-1", stringsRepeat()); err != nil {
		t.Fatal(err)
	}
	if err := appendTransparency(dir, "rel-2", stringsRepeat()); err == nil {
		t.Fatal("same digest logged under a second release")
	}
	if err := os.WriteFile(filepath.Join(dir, "transparency.log"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendTransparency(dir, "rel-3", stringsRepeat()); err == nil {
		t.Fatal("broken transparency chain accepted")
	}
}

func TestSBOMSignatureAndMeasuredBoot(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	body := []byte("spdx-bytes\n")
	path := filepath.Join(t.TempDir(), "sbom")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	rel := Release{SBOMKeyID: "sbom", SBOMSignature: ed25519.Sign(priv, body)}
	c := Config{TrustKeys: map[string]string{"sbom": base64.StdEncoding.EncodeToString(pub)}}
	if err := verifySBOMSignature(path, rel, c); err != nil {
		t.Fatal(err)
	}
	rel.SBOMSignature = ed25519.Sign(priv, []byte("other"))
	if err := verifySBOMSignature(path, rel, c); err == nil {
		t.Fatal("bad SBOM signature accepted")
	}
	h := newHarness(t)
	var signed Release
	if err := StrictJSON(h.assignment.Release.Payload, &signed); err != nil {
		t.Fatal(err)
	}
	signed.RequiresMeasuredBoot = true
	env, err := SignRelease(signed, "test", h.key)
	if err != nil {
		t.Fatal(err)
	}
	h.assignment.Release = env
	if _, err = h.e.Submit(h.assignment); err == nil {
		t.Fatal("measured boot quote was not required")
	}
	h.e.MeasuredBoot = func(context.Context) error { return nil }
	if _, err = h.e.Submit(h.assignment); err != nil {
		t.Fatal(err)
	}
}

func testKeys(t *testing.T, ids ...string) (map[string]ed25519.PrivateKey, map[string]string) {
	t.Helper()
	priv := map[string]ed25519.PrivateKey{}
	pubs := map[string]string{}
	for _, id := range ids {
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		priv[id] = key
		pubs[id] = base64.StdEncoding.EncodeToString(pub)
	}
	return priv, pubs
}

func signMeta(payload []byte, keys map[string]ed25519.PrivateKey, ids ...string) multiSigned {
	doc := multiSigned{Payload: payload}
	for _, id := range ids {
		doc.Signatures = append(doc.Signatures, keySignature{KeyID: id, Signature: ed25519.Sign(keys[id], payload)})
	}
	return doc
}

func writeMeta(t *testing.T, path string, doc multiSigned) {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func stringsRepeat() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
