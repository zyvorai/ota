// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Optional supply-chain metadata sits on top of the release envelope.
// Nothing in this file is a conformance claim.

type multiSigned struct {
	Payload    []byte         `json:"payload"`
	Signatures []keySignature `json:"signatures"`
}

type keySignature struct {
	KeyID     string `json:"key_id"`
	Signature []byte `json:"signature"`
}

type metaRole struct {
	Threshold int      `json:"threshold"`
	KeyIDs    []string `json:"key_ids"`
}

type delegation struct {
	Name      string   `json:"name"`
	Threshold int      `json:"threshold"`
	KeyIDs    []string `json:"key_ids"`
	Types     []string `json:"types"`
}

type rootMeta struct {
	Schema      int                 `json:"schema"`
	Version     uint64              `json:"version"`
	Expires     time.Time           `json:"expires"`
	Keys        map[string]string   `json:"keys"`
	Roles       map[string]metaRole `json:"roles"`
	Delegations []delegation        `json:"delegations,omitempty"`
}

type snapshotMeta struct {
	Version        uint64    `json:"version"`
	Expires        time.Time `json:"expires"`
	TargetsVersion uint64    `json:"targets_version"`
	TargetsSHA256  string    `json:"targets_sha256"`
}

type timestampMeta struct {
	Version         uint64    `json:"version"`
	Expires         time.Time `json:"expires"`
	SnapshotVersion uint64    `json:"snapshot_version"`
	SnapshotSHA256  string    `json:"snapshot_sha256"`
}

type targetsMeta struct {
	Version       uint64    `json:"version"`
	Expires       time.Time `json:"expires"`
	ReleaseSHA256 []string  `json:"release_sha256"`
	RejectSHA256  []string  `json:"reject_sha256,omitempty"`
}

type supplyDecision struct {
	active          bool
	rootPayload     []byte
	rootVersion     uint64
	snapshotVersion uint64
	snapshotSHA     string
	payloadSHA      string
}

func evaluateSupplyChain(c Config, db Database, releasePayload []byte, keyID string, r Release, now time.Time) (supplyDecision, error) {
	if c.TrustDir == "" {
		return supplyDecision{}, nil
	}
	root, rootPayload, err := loadRoot(c.TrustDir, c.RootSHA256, db.RootPayload, now)
	if err != nil {
		return supplyDecision{}, err
	}
	tsPayload, tsDoc, err := readSigned(filepath.Join(c.TrustDir, "timestamp.json"))
	if err != nil {
		return supplyDecision{}, err
	}
	var ts timestampMeta
	if err = StrictJSON(tsPayload, &ts); err != nil {
		return supplyDecision{}, errors.New("timestamp metadata: " + err.Error())
	}
	if !ts.Expires.After(now) {
		return supplyDecision{}, errors.New("timestamp metadata expired")
	}
	if err = verifyThreshold(tsPayload, tsDoc.Signatures, root.Keys, root.Roles["timestamp"]); err != nil {
		return supplyDecision{}, errors.New("timestamp metadata: " + err.Error())
	}
	snapPayload, snapDoc, err := readSigned(filepath.Join(c.TrustDir, "snapshot.json"))
	if err != nil {
		return supplyDecision{}, err
	}
	sum := sha256.Sum256(snapPayload)
	snapSHA := hex.EncodeToString(sum[:])
	if snapSHA != ts.SnapshotSHA256 {
		return supplyDecision{}, errors.New("timestamp does not match the snapshot")
	}
	var snap snapshotMeta
	if err = StrictJSON(snapPayload, &snap); err != nil {
		return supplyDecision{}, errors.New("snapshot metadata: " + err.Error())
	}
	if snap.Version != ts.SnapshotVersion {
		return supplyDecision{}, errors.New("timestamp snapshot version mismatch")
	}
	if snap.Version < db.SnapshotVersion || (snap.Version == db.SnapshotVersion && db.SnapshotSHA != "" && snapSHA != db.SnapshotSHA) {
		return supplyDecision{}, errors.New("snapshot rollback")
	}
	if !snap.Expires.After(now) {
		return supplyDecision{}, errors.New("snapshot metadata expired")
	}
	if err = verifyThreshold(snapPayload, snapDoc.Signatures, root.Keys, root.Roles["snapshot"]); err != nil {
		return supplyDecision{}, errors.New("snapshot metadata: " + err.Error())
	}
	targetsPayload, targetsDoc, err := readSigned(filepath.Join(c.TrustDir, "targets.json"))
	if err != nil {
		return supplyDecision{}, err
	}
	tSum := sha256.Sum256(targetsPayload)
	if hex.EncodeToString(tSum[:]) != snap.TargetsSHA256 {
		return supplyDecision{}, errors.New("snapshot does not match the targets metadata")
	}
	var targets targetsMeta
	if err = StrictJSON(targetsPayload, &targets); err != nil {
		return supplyDecision{}, errors.New("targets metadata: " + err.Error())
	}
	if targets.Version != snap.TargetsVersion || !targets.Expires.After(now) {
		return supplyDecision{}, errors.New("targets metadata is stale")
	}
	if err = verifyThreshold(targetsPayload, targetsDoc.Signatures, root.Keys, root.Roles["targets"]); err != nil {
		return supplyDecision{}, errors.New("targets metadata: " + err.Error())
	}
	payloadSum := sha256.Sum256(releasePayload)
	payloadSHA := hex.EncodeToString(payloadSum[:])
	if !containsString(targets.ReleaseSHA256, payloadSHA) {
		return supplyDecision{}, errors.New("release is not in the signed targets metadata")
	}
	arts, err := releaseArtifacts(r)
	if err != nil {
		return supplyDecision{}, err
	}
	for _, a := range arts {
		if containsString(targets.RejectSHA256, a.SHA256) {
			return supplyDecision{}, errors.New("artifact rejected by vulnerability policy")
		}
	}
	if err = authorizeReleaseKey(root, keyID, releaseTypes(r)); err != nil {
		return supplyDecision{}, err
	}
	return supplyDecision{
		active:          true,
		rootPayload:     rootPayload,
		rootVersion:     root.Version,
		snapshotVersion: snap.Version,
		snapshotSHA:     snapSHA,
		payloadSHA:      payloadSHA,
	}, nil
}

func applySupplyChain(d *Database, stateDir string, decision supplyDecision, releaseID string) error {
	if !decision.active {
		return nil
	}
	d.RootPayload = append([]byte(nil), decision.rootPayload...)
	d.RootVersion = decision.rootVersion
	d.SnapshotVersion = decision.snapshotVersion
	d.SnapshotSHA = decision.snapshotSHA
	return appendTransparency(stateDir, releaseID, decision.payloadSHA)
}

func loadRoot(dir, pinned string, stored []byte, now time.Time) (rootMeta, []byte, error) {
	payload, doc, err := readSigned(filepath.Join(dir, "root.json"))
	if err != nil {
		return rootMeta{}, nil, err
	}
	var candidate rootMeta
	if err = StrictJSON(payload, &candidate); err != nil {
		return rootMeta{}, nil, errors.New("root metadata: " + err.Error())
	}
	if candidate.Schema != 1 || candidate.Version == 0 || !candidate.Expires.After(now) {
		return rootMeta{}, nil, errors.New("root metadata is stale")
	}
	if len(stored) == 0 {
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != pinned {
			return rootMeta{}, nil, errors.New("root metadata does not match the pinned digest")
		}
		if err = verifyThreshold(payload, doc.Signatures, candidate.Keys, candidate.Roles["root"]); err != nil {
			return rootMeta{}, nil, errors.New("root metadata: " + err.Error())
		}
		return candidate, payload, nil
	}
	var current rootMeta
	if err = StrictJSON(stored, &current); err != nil {
		return rootMeta{}, nil, errors.New("stored root metadata: " + err.Error())
	}
	if candidate.Version < current.Version {
		return rootMeta{}, nil, errors.New("root rollback")
	}
	if candidate.Version == current.Version {
		if sha256.Sum256(payload) != sha256.Sum256(stored) {
			return rootMeta{}, nil, errors.New("root version reused with different bytes")
		}
		return current, stored, nil
	}
	if err = verifyThreshold(payload, doc.Signatures, current.Keys, current.Roles["root"]); err != nil {
		return rootMeta{}, nil, errors.New("root update: " + err.Error())
	}
	if err = verifyThreshold(payload, doc.Signatures, candidate.Keys, candidate.Roles["root"]); err != nil {
		return rootMeta{}, nil, errors.New("root update is not signed by its new root keys")
	}
	return candidate, payload, nil
}

func authorizeReleaseKey(root rootMeta, keyID string, types []string) error {
	if role, ok := root.Roles["targets"]; ok && containsString(role.KeyIDs, keyID) {
		return nil
	}
	for _, d := range root.Delegations {
		if !containsString(d.KeyIDs, keyID) {
			continue
		}
		for _, typ := range types {
			if !containsString(d.Types, typ) {
				return errors.New("delegated key cannot sign " + typ)
			}
		}
		return nil
	}
	return errors.New("release key is not authorized")
}

func releaseTypes(r Release) []string {
	if r.Schema == 1 {
		return []string{"os.rauc"}
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range r.Targets {
		if seen[t.Type] {
			continue
		}
		seen[t.Type] = true
		out = append(out, t.Type)
	}
	return out
}

func verifyThreshold(payload []byte, sigs []keySignature, keys map[string]string, role metaRole) error {
	if role.Threshold < 1 || len(role.KeyIDs) < role.Threshold {
		return errors.New("role threshold is not configured")
	}
	seen := map[string]bool{}
	for _, sig := range sigs {
		if !containsString(role.KeyIDs, sig.KeyID) || seen[sig.KeyID] {
			continue
		}
		pub, err := decodeKey(keys[sig.KeyID])
		if err != nil {
			return err
		}
		if !ed25519.Verify(pub, payload, sig.Signature) {
			return errors.New("invalid signature from " + sig.KeyID)
		}
		seen[sig.KeyID] = true
	}
	if len(seen) < role.Threshold {
		return errors.New("signature threshold not met")
	}
	return nil
}

func verifySBOMSignature(path string, r Release, c Config) error {
	if r.SBOMKeyID == "" && len(r.SBOMSignature) == 0 && !c.RequireSBOMSignature {
		return nil
	}
	if r.SBOMKeyID == "" || len(r.SBOMSignature) == 0 {
		return errors.New("signed SBOM is required")
	}
	encoded := c.TrustKeys[r.SBOMKeyID]
	if encoded == "" {
		return errors.New("SBOM signing key is not pinned")
	}
	pub, err := decodeKey(encoded)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, b, r.SBOMSignature) {
		return errors.New("SBOM signature does not match")
	}
	return nil
}

func readSigned(path string) ([]byte, multiSigned, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, multiSigned{}, err
	}
	var doc multiSigned
	if err = StrictJSON(b, &doc); err != nil {
		return nil, multiSigned{}, err
	}
	if len(doc.Payload) == 0 || len(doc.Signatures) == 0 {
		return nil, multiSigned{}, errors.New("metadata signature missing")
	}
	return doc.Payload, doc, nil
}

func decodeKey(encoded string) (ed25519.PublicKey, error) {
	pub, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("invalid trust key")
	}
	return pub, nil
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

type transparencyLine struct {
	Prev          string `json:"prev"`
	ReleaseID     string `json:"release_id"`
	PayloadSHA256 string `json:"payload_sha256"`
}

func appendTransparency(dir, releaseID, payloadSHA string) error {
	path := filepath.Join(dir, "transparency.log")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	prev := strings.Repeat("0", 64)
	text := string(b)
	if text != "" && !strings.HasSuffix(text, "\n") {
		return errors.New("transparency log failed")
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		var row transparencyLine
		if err = json.Unmarshal([]byte(line), &row); err != nil || row.Prev != prev {
			return errors.New("transparency log failed")
		}
		if row.PayloadSHA256 == payloadSHA && row.ReleaseID != releaseID {
			return errors.New("transparency log already has this release digest")
		}
		if row.PayloadSHA256 == payloadSHA && row.ReleaseID == releaseID {
			return nil
		}
		sum := sha256.Sum256([]byte(line + "\n"))
		prev = hex.EncodeToString(sum[:])
	}
	row, err := json.Marshal(transparencyLine{Prev: prev, ReleaseID: releaseID, PayloadSHA256: payloadSHA})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(append(row, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
