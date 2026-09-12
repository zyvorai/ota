// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Slot struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Good    bool   `json:"good"`
}
type DeviceStatus struct {
	BootID     string `json:"boot_id"`
	Booted     string `json:"booted"`
	Primary    string `json:"primary"`
	Operation  string `json:"operation"`
	Compatible string `json:"compatible"`
	Slots      []Slot `json:"slots"`
}

func (s DeviceStatus) Other() (string, error) {
	if len(s.Slots) != 2 {
		return "", errors.New("exactly two bootable rootfs slots required")
	}
	found := false
	other := ""
	for _, slot := range s.Slots {
		if slot.Name == s.Booted {
			found = true
		} else {
			other = slot.Name
		}
	}
	if !found || other == "" {
		return "", errors.New("cannot determine A/B slots")
	}
	return other, nil
}
func (s DeviceStatus) VersionOf(name string) string {
	for _, slot := range s.Slots {
		if slot.Name == name {
			return slot.Version
		}
	}
	return ""
}

type Backend interface {
	Status(context.Context) (DeviceStatus, error)
	Install(context.Context, string, Release) error
	Mark(context.Context, string, string) error
	Reboot(context.Context) error
}

func (s DeviceStatus) GoodOf(name string) bool {
	for _, slot := range s.Slots {
		if slot.Name == name {
			return slot.Good
		}
	}
	return false
}

type Simulator struct {
	mu          sync.Mutex
	path        string
	compatible  string
	FailInstall bool
	FailBoot    bool
}

func NewSimulator(dir, compatible string) (*Simulator, error) {
	s := &Simulator{path: filepath.Join(dir, "simulator.json"), compatible: compatible}
	if _, e := os.Stat(s.path); os.IsNotExist(e) {
		e = s.write(DeviceStatus{BootID: "boot-0", Booted: "rootfs.0", Primary: "rootfs.0", Operation: "idle", Compatible: compatible, Slots: []Slot{{"rootfs.0", "factory", true}, {"rootfs.1", "factory", true}}})
		if e != nil {
			return nil, e
		}
	}
	_, e := s.read()
	return s, e
}
func (s *Simulator) read() (DeviceStatus, error) {
	var v DeviceStatus
	b, e := os.ReadFile(s.path)
	if e == nil {
		e = StrictJSON(b, &v)
	}
	return v, e
}
func (s *Simulator) write(v DeviceStatus) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return AtomicWrite(s.path, b, 0600)
}
func (s *Simulator) Status(context.Context) (DeviceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}
func (s *Simulator) Install(ctx context.Context, path string, r Release) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.FailInstall {
		return errors.New("simulated install failure")
	}
	if e := CheckArtifact(path, r.Artifact); e != nil {
		return e
	}
	v, e := s.read()
	if e != nil {
		return e
	}
	target, e := v.Other()
	if e != nil {
		return e
	}
	for i := range v.Slots {
		if v.Slots[i].Name == target {
			v.Slots[i].Version = r.Version
			v.Slots[i].Good = false
		}
	}
	v.Primary = target
	return s.write(v)
}
func (s *Simulator) Mark(_ context.Context, state, slot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.read()
	if e != nil {
		return e
	}
	found := false
	for i := range v.Slots {
		if v.Slots[i].Name == slot {
			found = true
			switch state {
			case "good":
				v.Slots[i].Good = true
			case "bad":
				v.Slots[i].Good = false
			case "active":
				v.Primary = slot
			default:
				return errors.New("invalid mark state")
			}
		}
	}
	if !found {
		return errors.New("unknown slot")
	}
	return s.write(v)
}
func (s *Simulator) Reboot(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.read()
	if e != nil {
		return e
	}
	v.BootID += "x"
	if s.FailBoot {
		v.Primary = v.Booted
	} else {
		v.Booted = v.Primary
	}
	return s.write(v)
}
func LinuxBootID() (string, error) {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return "", e
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", errors.New("empty boot ID")
	}
	return v, nil
}
