// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func orderedTargets(targets []Target) ([]Target, error) {
	if err := topoTargets(targets); err != nil {
		return nil, err
	}
	incoming := map[string]int{}
	for _, t := range targets {
		incoming[t.ID] = len(t.DependsOn)
	}
	var ready []Target
	left := append([]Target(nil), targets...)
	var out []Target
	for len(out) < len(targets) {
		ready = ready[:0]
		var rest []Target
		for _, t := range left {
			if incoming[t.ID] == 0 {
				ready = append(ready, t)
			} else {
				rest = append(rest, t)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("target dependency cycle")
		}
		for _, t := range ready {
			out = append(out, t)
			for i := range rest {
				for _, dep := range rest[i].DependsOn {
					if dep == t.ID {
						incoming[rest[i].ID]--
					}
				}
			}
		}
		left = rest
	}
	return out, nil
}

func applySidePayloads(stateDir string, rel Release, checks []Check) error {
	order, err := orderedTargets(rel.Targets)
	if err != nil {
		return err
	}
	for _, t := range order {
		if t.Type == "os.rauc" {
			continue
		}
		src := filepath.Join(stateDir, "cache", t.Artifact.SHA256+".raucb")
		if err = CheckArtifact(src, t.Artifact); err != nil {
			return err
		}
		dir := filepath.Join(stateDir, "payloads", t.ID)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if t.StorageBytes > 0 {
			var fs syscall.Statfs_t
			if err = syscall.Statfs(dir, &fs); err != nil {
				return err
			}
			if fs.Bavail*uint64(fs.Bsize) < uint64(t.StorageBytes) {
				return errors.New("payload storage requirement not met")
			}
		}
		current := filepath.Join(dir, "current")
		previous := filepath.Join(dir, "previous")
		if _, err = os.Stat(current); err == nil {
			_ = os.Remove(previous)
			if err = os.Rename(current, previous); err != nil {
				return err
			}
		}
		if err = copyFile(src, current); err != nil {
			return err
		}
		if t.Health != "" {
			var selected []Check
			for _, c := range checks {
				if c.Name == t.Health {
					selected = append(selected, c)
				}
			}
			if len(selected) == 0 {
				return errors.New("payload health check not configured: " + t.Health)
			}
			if err = LocalHealth(selected)(context.Background()); err != nil {
				return err
			}
		}
	}
	return nil
}

func rollbackSidePayloads(stateDir string, rel Release) error {
	for _, t := range rel.Targets {
		if t.Type == "os.rauc" {
			continue
		}
		dir := filepath.Join(stateDir, "payloads", t.ID)
		current := filepath.Join(dir, "current")
		previous := filepath.Join(dir, "previous")
		if _, err := os.Stat(previous); err == nil {
			_ = os.Remove(current)
			if err = os.Rename(previous, current); err != nil {
				return err
			}
			continue
		}
		_ = os.Remove(current)
	}
	return nil
}

func commitSidePayloads(stateDir string, rel Release) {
	for _, t := range rel.Targets {
		if t.Type == "os.rauc" {
			continue
		}
		_ = os.Remove(filepath.Join(stateDir, "payloads", t.ID, "previous"))
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
