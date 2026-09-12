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
	if r.Schema != 1 || !identifier.MatchString(r.ID) || !identifier.MatchString(r.Version) || r.Sequence == 0 {
		return r, errors.New("invalid release metadata")
	}
	if r.Compatible != c.Compatible || r.Backend != c.Backend {
		return r, errors.New("release incompatible with device/backend")
	}
	if !r.Expires.After(now) {
		return r, errors.New("release expired")
	}
	if !digestPattern.MatchString(r.Artifact.SHA256) || r.Artifact.Size <= 0 || r.Artifact.Size > c.MaxArtifactBytes {
		return r, errors.New("invalid artifact size/digest")
	}
	if r.SBOMSHA256 != "" && !digestPattern.MatchString(r.SBOMSHA256) {
		return r, errors.New("invalid SBOM digest")
	}
	return r, nil
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
