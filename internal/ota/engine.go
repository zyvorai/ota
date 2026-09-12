// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Engine struct {
	Config       Config
	Store        *Store
	Backend      Backend
	Downloader   Downloader
	Health       HealthFunc
	Now          func() time.Time
	mu           sync.Mutex
	healthySince time.Time
}

func NewEngine(c Config, s *Store, b Backend) *Engine {
	return &Engine{Config: c, Store: s, Backend: b, Downloader: Downloader{Config: c}, Health: LocalHealth(c.Checks), Now: time.Now}
}
func (e *Engine) Submit(a Assignment) (Job, error) {
	if !e.mu.TryLock() {
		return Job{}, errors.New("update engine busy; retry")
	}
	defer e.mu.Unlock()
	if !identifier.MatchString(a.JobID) || a.DeviceID != e.Config.DeviceID {
		return Job{}, errors.New("invalid job/device")
	}
	fingerprint := Fingerprint(a)
	db := e.Store.View()
	if j, ok := db.Jobs[a.JobID]; ok {
		if j.Fingerprint == fingerprint {
			return j, nil
		}
		return Job{}, errors.New("job ID reused with different assignment")
	}
	now := e.Now()
	r, err := VerifyEnvelope(a.Release, e.Config, now)
	if err != nil {
		return Job{}, err
	}
	if !a.Deadline.After(now) || !a.Deadline.After(a.NotBefore) || a.Deadline.After(r.Expires) {
		return Job{}, errors.New("invalid assignment window")
	}
	j := Job{Assignment: a, Release: r, Fingerprint: fingerprint, State: Accepted, Updated: now}
	err = e.Store.Update(func(d *Database) error {
		if d.Active != "" {
			return errors.New("another update is active")
		}
		if len(d.Jobs) >= 10000 || len(d.Events) > 9000 {
			return errors.New("journal capacity reached; export/ack events or reprovision journal under documented procedure")
		}
		if r.Sequence <= d.HighSequence {
			return errors.New("replayed or downgraded release sequence")
		}
		d.HighSequence = r.Sequence
		d.Active = a.JobID
		d.Jobs[a.JobID] = j
		return addEvent(d, j, now)
	})
	return j, err
}
func addEvent(d *Database, j Job, now time.Time) error {
	if len(d.Events) >= 10000 {
		return errors.New("event outbox full; update paused")
	}
	d.EventSequence++
	d.Events = append(d.Events, Event{Sequence: d.EventSequence, JobID: j.Assignment.JobID, State: j.State, Error: j.Error, Time: now})
	return nil
}
func (e *Engine) save(j Job, state State, message string) error {
	j.State = state
	j.Error = message
	j.Updated = e.Now()
	return e.Store.Update(func(d *Database) error {
		d.Jobs[j.Assignment.JobID] = j
		if Terminal(state) {
			d.Active = ""
		}
		return addEvent(d, j, j.Updated)
	})
}
func (e *Engine) current() (Job, bool) { d := e.Store.View(); j, ok := d.Jobs[d.Active]; return j, ok }
func (e *Engine) Step(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, ok := e.current()
	if !ok {
		return e.confirmNormalBoot(ctx)
	}
	now := e.Now()
	switch j.State {
	case Accepted, Downloading, Verified:
		if !now.Before(j.Assignment.Deadline) {
			return e.save(j, Failed, "assignment expired before installation")
		}
		if now.Before(j.Assignment.NotBefore) {
			return nil
		}
		if _, err := VerifyEnvelope(j.Assignment.Release, e.Config, now); err != nil {
			return e.save(j, Failed, err.Error())
		}
	}
	switch j.State {
	case Accepted:
		return e.save(j, Downloading, "")
	case Downloading:
		if _, err := e.Downloader.Fetch(ctx, j.Release.Artifact); err != nil {
			return err
		}
		return e.save(j, Verified, "")
	case Verified:
		s, err := e.Backend.Status(ctx)
		if err != nil {
			return err
		}
		if s.Operation != "idle" {
			return errors.New("backend busy")
		}
		if s.Compatible != e.Config.Compatible {
			return e.save(j, Failed, "backend compatibility mismatch")
		}
		target, err := s.Other()
		if err != nil {
			return e.save(j, Failed, err.Error())
		}
		// Require distinct versions to make post-boot version evidence unambiguous.
		for _, slot := range s.Slots {
			if slot.Version == j.Release.Version {
				return e.save(j, Failed, "release version already exists on device")
			}
		}
		path := filepath.Join(e.Config.StateDir, "cache", j.Release.Artifact.SHA256+".raucb")
		if err = CheckArtifact(path, j.Release.Artifact); err != nil {
			return e.save(j, Failed, err.Error())
		}
		if !e.Now().Before(j.Assignment.Deadline) || !e.Now().Before(j.Release.Expires) {
			return e.save(j, Failed, "installation window expired during verification")
		}
		j.OldSlot = s.Booted
		j.OldVersion = s.VersionOf(s.Booted)
		j.TargetSlot = target
		j.BootID = s.BootID
		if err = e.save(j, Installing, ""); err != nil {
			return err
		}
		installCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		if err = e.Backend.Install(installCtx, path, j.Release); err != nil {
			return e.save(j, NeedsRecovery, "installation outcome requires inspection: "+err.Error())
		}
		s, err = e.Backend.Status(ctx)
		if err != nil {
			return e.save(j, NeedsRecovery, "unable to inspect installation result")
		}
		if s.Operation != "idle" || s.Booted != j.OldSlot || s.Primary != j.TargetSlot || s.VersionOf(j.TargetSlot) != j.Release.Version {
			return e.save(j, NeedsRecovery, "installed slot/version evidence mismatch")
		}
		return e.save(j, AwaitingReboot, "")
	case Installing:
		// A process death can occur anywhere between RAUC accepting and completing a
		// request. Never reinstall or declare success based only on a journal record.
		return e.save(j, NeedsRecovery, "agent restarted during installation; inspect RAUC and use recover-abort")
	case AwaitingReboot:
		s, err := e.Backend.Status(ctx)
		if err != nil {
			return err
		}
		if s.BootID == j.BootID {
			if s.Booted != j.OldSlot {
				return e.save(j, NeedsRecovery, "slot changed without a boot ID change")
			}
			if !j.RebootRequested && j.Assignment.AutoReboot && !now.Before(j.Assignment.NotBefore) && now.Before(j.Assignment.Deadline) {
				return e.reboot(ctx, j)
			}
			return nil
		}
		if s.Booted == j.OldSlot {
			return e.save(j, RolledBack, "bootloader returned to previous slot")
		}
		if s.Booted != j.TargetSlot || s.VersionOf(j.TargetSlot) != j.Release.Version {
			return e.save(j, NeedsRecovery, "unexpected booted slot/version")
		}
		j.HealthStarted = now
		j.BootID = s.BootID
		e.healthySince = time.Time{}
		return e.save(j, CheckingHealth, "")
	case CheckingHealth, Committing:
		s, err := e.Backend.Status(ctx)
		if err != nil {
			return err
		}
		if s.Booted == j.OldSlot {
			return e.save(j, RolledBack, "bootloader returned to previous slot during verification")
		}
		if s.Booted != j.TargetSlot || s.VersionOf(j.TargetSlot) != j.Release.Version {
			return e.save(j, NeedsRecovery, "health/commit slot mismatch")
		}
		if s.BootID != j.BootID {
			j.BootID = s.BootID
			e.healthySince = time.Time{}
			return e.save(j, CheckingHealth, "rechecking health after another boot")
		}
		if j.State == Committing {
			// Mark good is idempotent; the durable committing intent is written only
			// after health passes. Recover an interrupted mark without running install.
			if err = e.Backend.Mark(ctx, "good", j.TargetSlot); err != nil {
				return err
			}
			return e.save(j, Committed, "")
		}
		if now.Before(j.HealthStarted) || !now.Before(j.HealthStarted.Add(time.Duration(e.Config.HealthTimeoutSeconds)*time.Second)) {
			return e.save(j, RollbackPending, "health verification deadline exceeded")
		}
		if err = e.Health(ctx); err != nil {
			e.healthySince = time.Time{}
			return nil
		}
		if e.healthySince.IsZero() {
			e.healthySince = now
		}
		if now.Sub(e.healthySince) < time.Duration(e.Config.HealthStableSeconds)*time.Second {
			return nil
		}
		return e.save(j, Committing, "")
	case RollbackPending:
		s, err := e.Backend.Status(ctx)
		if err != nil {
			return err
		}
		if s.Operation != "idle" {
			return errors.New("backend busy during rollback")
		}
		if s.Booted == j.OldSlot {
			return e.save(j, RolledBack, j.Error)
		}
		if s.Booted != j.TargetSlot {
			return e.save(j, NeedsRecovery, "rollback slot mismatch")
		}
		if err = e.Backend.Mark(ctx, "bad", j.TargetSlot); err != nil {
			return err
		}
		if err = e.Backend.Mark(ctx, "active", j.OldSlot); err != nil {
			return err
		}
		return e.Backend.Reboot(ctx)
	case NeedsRecovery:
		return nil
	default:
		return fmt.Errorf("unknown active state %s", j.State)
	}
}
func (e *Engine) reboot(ctx context.Context, j Job) error {
	j.RebootRequested = true
	if err := e.save(j, AwaitingReboot, ""); err != nil {
		return err
	}
	if err := e.Backend.Reboot(ctx); err != nil {
		j.RebootRequested = false
		_ = e.save(j, AwaitingReboot, "reboot request failed")
		return err
	}
	return nil
}
func (e *Engine) RequestReboot(ctx context.Context) error {
	if !e.mu.TryLock() {
		return errors.New("engine busy")
	}
	defer e.mu.Unlock()
	j, ok := e.current()
	if !ok || j.State != AwaitingReboot {
		return errors.New("no update awaiting reboot")
	}
	now := e.Now()
	if now.Before(j.Assignment.NotBefore) || !now.Before(j.Assignment.Deadline) {
		return errors.New("outside reboot window")
	}
	s, err := e.Backend.Status(ctx)
	if err != nil {
		return err
	}
	if s.BootID != j.BootID || s.Booted != j.OldSlot || s.Operation != "idle" {
		return errors.New("device has already rebooted or backend is busy")
	}
	return e.reboot(ctx, j)
}

// RecoverAbort is deliberately operator-only. It does not infer success from a
// partially installed image; it restores the original slot as next boot target.
func (e *Engine) RecoverAbort(ctx context.Context) error {
	if !e.mu.TryLock() {
		return errors.New("engine busy")
	}
	defer e.mu.Unlock()
	j, ok := e.current()
	if !ok || j.State != NeedsRecovery {
		return errors.New("no job needs recovery")
	}
	s, err := e.Backend.Status(ctx)
	if err != nil {
		return err
	}
	if s.Operation != "idle" || s.Booted != j.OldSlot || j.TargetSlot == "" || j.TargetSlot == j.OldSlot {
		return errors.New("recover-abort requires idle backend on original slot")
	}
	if err = e.Backend.Mark(ctx, "bad", j.TargetSlot); err != nil {
		return err
	}
	if err = e.Backend.Mark(ctx, "active", j.OldSlot); err != nil {
		return err
	}
	return e.save(j, Failed, "operator aborted ambiguous installation; previous slot restored")
}
func (e *Engine) Ack(sequence uint64) error {
	return e.Store.Update(func(d *Database) error {
		if sequence > d.EventSequence {
			return errors.New("ack exceeds latest event")
		}
		i := 0
		for i < len(d.Events) && d.Events[i].Sequence <= sequence {
			i++
		}
		d.Events = append([]Event(nil), d.Events[i:]...)
		return nil
	})
}
func (e *Engine) GC() error {
	if !e.mu.TryLock() {
		return errors.New("engine busy")
	}
	defer e.mu.Unlock()
	if _, ok := e.current(); ok {
		return errors.New("cache cleanup requires no active job")
	}
	dir := filepath.Join(e.Config.StateDir, "cache")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
