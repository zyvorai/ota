// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ExportCampaign writes a signed assignment and every artifact it names.
// artifactDir must contain each file as {sha256}.raucb. The signed payload is not rewritten.
func ExportCampaign(dir string, assignment Assignment, artifactDir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	arts, err := assignmentArtifacts(assignment)
	if err != nil {
		return err
	}
	for _, a := range arts {
		src := filepath.Join(artifactDir, a.SHA256+".raucb")
		if err = CheckArtifact(src, a); err != nil {
			return err
		}
		if err = copyFile(src, filepath.Join(dir, a.SHA256+".raucb")); err != nil {
			return err
		}
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

func assignmentArtifacts(a Assignment) ([]Artifact, error) {
	rel, err := assignmentRelease(a)
	if err != nil {
		return nil, err
	}
	return releaseArtifacts(rel)
}

func releaseArtifacts(r Release) ([]Artifact, error) {
	var out []Artifact
	seen := map[string]bool{}
	add := func(a Artifact) error {
		if a.SHA256 == "" && a.URL == "" && a.Size == 0 {
			return nil
		}
		if !digestPattern.MatchString(a.SHA256) || a.Size <= 0 {
			return errors.New("campaign artifact missing digest")
		}
		if seen[a.SHA256] {
			return nil
		}
		seen[a.SHA256] = true
		out = append(out, a)
		return nil
	}
	if err := add(r.Artifact); err != nil {
		return nil, err
	}
	if err := add(r.SBOM); err != nil {
		return nil, err
	}
	for _, t := range r.Targets {
		if err := add(t.Artifact); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("campaign has no artifact")
	}
	return out, nil
}

// ImportCampaign reads an offline campaign directory and checks every artifact
// digest. The caller still verifies the envelope with the device trust keys before submit.
func ImportCampaign(dir string) (Assignment, error) {
	b, err := os.ReadFile(filepath.Join(dir, "assignment.json"))
	if err != nil {
		return Assignment{}, err
	}
	var a Assignment
	if err = StrictJSON(b, &a); err != nil {
		return Assignment{}, err
	}
	arts, err := assignmentArtifacts(a)
	if err != nil {
		return Assignment{}, err
	}
	for _, art := range arts {
		if err = CheckArtifact(filepath.Join(dir, art.SHA256+".raucb"), art); err != nil {
			return Assignment{}, err
		}
	}
	return a, nil
}

// CopyToMedia places one artifact under local_media_dir. The signed URL is unchanged.
func CopyToMedia(mediaDir, artifactPath string, a Artifact) (string, error) {
	if !filepath.IsAbs(mediaDir) {
		return "", errors.New("media dir must be absolute")
	}
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

// InstallCampaign copies every campaign artifact into mediaDir for local_media_dir.
func InstallCampaign(mediaDir, campaignDir string, assignment Assignment) error {
	arts, err := assignmentArtifacts(assignment)
	if err != nil {
		return err
	}
	for _, a := range arts {
		if _, err = CopyToMedia(mediaDir, filepath.Join(campaignDir, a.SHA256+".raucb"), a); err != nil {
			return err
		}
	}
	return nil
}
