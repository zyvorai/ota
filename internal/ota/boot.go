// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"errors"
	"time"
)

// Normal boots also need mark-good on bootloaders with boot-attempt counters.
// Only confirm versions associated with a completed OTA transaction. Factory
// image confirmation remains part of board provisioning until the first update.
func (e *Engine) confirmNormalBoot(ctx context.Context) error {
	d := e.Store.View()
	if len(d.Jobs) == 0 {
		return nil
	}
	s, err := e.Backend.Status(ctx)
	if err != nil {
		return err
	}
	if s.Operation != "idle" {
		return nil
	}
	if d.BootCheck.BootID == s.BootID && d.BootCheck.Confirmed {
		return nil
	}
	known := map[string]string{}
	// Pick the latest completed job, not arbitrary map iteration order.
	var latest Job
	for _, j := range d.Jobs {
		if (j.State == Committed || j.State == RolledBack) && j.Release.Sequence > latest.Release.Sequence {
			latest = j
		}
	}
	if latest.Assignment.JobID == "" {
		return nil
	}
	known[latest.OldSlot] = latest.OldVersion
	if latest.State == Committed {
		known[latest.TargetSlot] = latest.Release.Version
	}
	if v, ok := known[s.Booted]; !ok || v != s.VersionOf(s.Booted) {
		return errors.New("unrecognized normal boot; refusing mark-good")
	}
	now := e.Now()
	if d.BootCheck.BootID != s.BootID {
		e.healthySince = time.Time{}
		return e.Store.Update(func(d *Database) error { d.BootCheck = BootCheck{BootID: s.BootID, Started: now}; return nil })
	}
	if now.Before(d.BootCheck.Started) || !now.Before(d.BootCheck.Started.Add(time.Duration(e.Config.HealthTimeoutSeconds)*time.Second)) {
		other, err := s.Other()
		if err != nil {
			return err
		}
		if version, ok := known[other]; !ok || version != s.VersionOf(other) || !s.GoodOf(other) {
			return errors.New("normal boot health failed and no known fallback exists; board recovery required")
		}
		if err = e.Backend.Mark(ctx, "bad", s.Booted); err != nil {
			return err
		}
		if err = e.Backend.Mark(ctx, "active", other); err != nil {
			return err
		}
		return e.Backend.Reboot(ctx)
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
	if err = e.Backend.Mark(ctx, "good", s.Booted); err != nil {
		return err
	}
	return e.Store.Update(func(d *Database) error { d.BootCheck.Confirmed = true; return nil })
}
