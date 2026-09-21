// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ExportCampaign writes a signed assignment and its artifact for offline or USB use.
// The artifact is copied and re-checked against the signed digest. Import uses the same bytes.
func ExportCampaign(dir string, assignment Assignment, artifactPath string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	rel, err := assignmentRelease(assignment)
	if err != nil {
		return err
	}
	if err = CheckArtifact(artifactPath, rel.Artifact); err != nil && len(rel.Targets) == 0 {
		return err
	}
	src := artifactPath
	name := rel.Artifact.SHA256
	if name == "" && len(rel.Targets) > 0 {
		name = rel.Targets[0].Artifact.SHA256
		if err = CheckArtifact(artifactPath, rel.Targets[0].Artifact); err != nil {
			return err
		}
	} else if err = CheckArtifact(artifactPath, rel.Artifact); err != nil {
		return err
	}
	if name == "" {
		return errors.New("campaign has no artifact")
	}
	dst := filepath.Join(dir, name+".raucb")
	if err = copyFile(src, dst); err != nil {
		return err
	}
	b, err := json.MarshalIndent(assignment, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(dir, "assignment.json"), b, 0600)
}

func assignmentRelease(a Assignment) (Release, error) {
	var r Release
	if err := StrictJSON(a.Release.Payload, &r); err != nil {
		return r, err
	}
	return r, nil
}

// ImportCampaign reads an offline campaign directory. The caller still verifies
// the envelope with the device trust keys before submit.
func ImportCampaign(dir string) (Assignment, string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "assignment.json"))
	if err != nil {
		return Assignment{}, "", err
	}
	var a Assignment
	if err = StrictJSON(b, &a); err != nil {
		return Assignment{}, "", err
	}
	rel, err := assignmentRelease(a)
	if err != nil {
		return Assignment{}, "", err
	}
	digest := rel.Artifact.SHA256
	art := rel.Artifact
	if digest == "" && len(rel.Targets) > 0 {
		digest = rel.Targets[0].Artifact.SHA256
		art = rel.Targets[0].Artifact
	}
	path := filepath.Join(dir, digest+".raucb")
	if err = CheckArtifact(path, art); err != nil {
		return Assignment{}, "", err
	}
	return a, path, nil
}

// CopyToMedia places an imported artifact under local_media_dir for a file:// install.
func CopyToMedia(mediaDir, artifactPath string, a Artifact) (string, error) {
	if err := CheckArtifact(artifactPath, a); err != nil {
		return "", err
	}
	if err := os.MkdirAll(mediaDir, 0700); err != nil {
		return "", err
	}
	dst := filepath.Join(mediaDir, a.SHA256+".raucb")
	if err := copyFile(artifactPath, dst); err != nil {
		return "", err
	}
	return dst, nil
}
