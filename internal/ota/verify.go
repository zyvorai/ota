// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func VerifyEnvelope(env Envelope, c Config, now time.Time) (Release, error) {
	var r Release
	encoded, ok := c.TrustKeys[env.KeyID]
	if !ok {
		return r, errors.New("untrusted signing key")
	}
	pub, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return r, errors.New("invalid trust key")
	}
	if len(env.Payload) > 64<<10 || !ed25519.Verify(pub, env.Payload, env.Signature) {
		return r, errors.New("invalid release signature")
	}
	if err = StrictJSON(env.Payload, &r); err != nil {
		return r, fmt.Errorf("release: %w", err)
	}
	if (r.Schema != 1 && r.Schema != 2) || !identifier.MatchString(r.ID) || !identifier.MatchString(r.Version) || r.Sequence == 0 {
		return r, errors.New("invalid release metadata")
	}
	if r.Compatible != c.Compatible || r.Backend != c.Backend {
		return r, errors.New("release incompatible with device/backend")
	}
	if !r.Expires.After(now) {
		return r, errors.New("release expired")
	}
	if r.Adaptive && !c.AllowAdaptive {
		return r, errors.New("adaptive updates are not enabled for this device")
	}
	if r.SBOMSHA256 != "" && !digestPattern.MatchString(r.SBOMSHA256) {
		return r, errors.New("invalid SBOM digest")
	}
	if r.Schema == 1 {
		if len(r.Targets) != 0 {
			return r, errors.New("schema 1 release cannot carry targets")
		}
		if err = checkArtifact(r.Artifact, c.MaxArtifactBytes); err != nil {
			return r, err
		}
		return r, nil
	}
	if len(r.Targets) == 0 {
		return r, errors.New("schema 2 release requires targets")
	}
	seen := map[string]bool{}
	for i := range r.Targets {
		t := &r.Targets[i]
		if !identifier.MatchString(t.ID) || seen[t.ID] {
			return r, errors.New("invalid target id")
		}
		seen[t.ID] = true
		switch t.Type {
		case "os.rauc", "container.oci", "config.bundle", "model.oci":
		default:
			return r, errors.New("unsupported target type")
		}
		if t.Type == "os.rauc" && t.Reboot != "required" {
			return r, errors.New("os.rauc requires reboot")
		}
		if t.Type != "os.rauc" && t.Reboot != "" && t.Reboot != "none" {
			return r, errors.New("payload reboot must be none")
		}
		if t.Rollback != "" && t.Rollback != "previous-digest" && t.Rollback != "slot" {
			return r, errors.New("unsupported rollback behavior")
		}
		if t.Requires != "" && !containsCap(c.Capabilities, t.Requires) {
			return r, errors.New("device missing required capability")
		}
		if err = checkArtifact(t.Artifact, c.MaxArtifactBytes); err != nil {
			return r, err
		}
		if t.StorageBytes < 0 {
			return r, errors.New("invalid storage requirement")
		}
	}
	if err = topoTargets(r.Targets); err != nil {
		return r, err
	}
	if osTarget, ok := r.OSTarget(); ok {
		r.Artifact = osTarget.Artifact
	} else if r.Artifact.URL != "" {
		if err = checkArtifact(r.Artifact, c.MaxArtifactBytes); err != nil {
			return r, err
		}
	}
	return r, nil
}

func checkArtifact(a Artifact, max int64) error {
	if !digestPattern.MatchString(a.SHA256) || a.Size <= 0 || a.Size > max || a.URL == "" {
		return errors.New("invalid artifact size/digest")
	}
	return nil
}

func containsCap(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func topoTargets(targets []Target) error {
	incoming := map[string]int{}
	byID := map[string]Target{}
	for _, t := range targets {
		byID[t.ID] = t
		if _, ok := incoming[t.ID]; !ok {
			incoming[t.ID] = 0
		}
	}
	for _, t := range targets {
		for _, dep := range t.DependsOn {
			if _, ok := byID[dep]; !ok {
				return errors.New("target dependency missing")
			}
			incoming[t.ID]++
		}
	}
	var ready []string
	for id, n := range incoming {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	seen := 0
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		seen++
		for _, t := range targets {
			for _, dep := range t.DependsOn {
				if dep == id {
					incoming[t.ID]--
					if incoming[t.ID] == 0 {
						ready = append(ready, t.ID)
					}
				}
			}
		}
	}
	if seen != len(targets) {
		return errors.New("target dependency cycle")
	}
	return nil
}
func SignRelease(r Release, id string, key ed25519.PrivateKey) (Envelope, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Envelope{}, errors.New("invalid private key")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{KeyID: id, Payload: b, Signature: ed25519.Sign(key, b)}, nil
}
func Fingerprint(a Assignment) string {
	b, _ := json.Marshal(a)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
