// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const raucService = "de.pengutronix.rauc"
const raucInterface = "de.pengutronix.rauc.Installer"
const raucPath = dbus.ObjectPath("/")

type RAUC struct {
	Conn   *dbus.Conn
	BootID func() (string, error)
}

func NewRAUC() (*RAUC, error) {
	c, e := dbus.ConnectSystemBus()
	if e != nil {
		return nil, e
	}
	return &RAUC{Conn: c, BootID: LinuxBootID}, nil
}
func (r *RAUC) property(ctx context.Context, name string) (dbus.Variant, error) {
	var v dbus.Variant
	e := r.Conn.Object(raucService, raucPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, raucInterface, name).Store(&v)
	return v, e
}
func (r *RAUC) stringProperty(ctx context.Context, name string) (string, error) {
	v, e := r.property(ctx, name)
	if e != nil {
		return "", e
	}
	s, ok := v.Value().(string)
	if !ok {
		return "", fmt.Errorf("RAUC property %s not a string", name)
	}
	return s, nil
}

type raucSlot struct {
	Name       string
	Properties map[string]dbus.Variant
}

func (r *RAUC) Status(ctx context.Context) (DeviceStatus, error) {
	var s DeviceStatus
	var e error
	if s.Operation, e = r.stringProperty(ctx, "Operation"); e != nil {
		return s, e
	}
	if s.Compatible, e = r.stringProperty(ctx, "Compatible"); e != nil {
		return s, e
	}
	if s.BootID, e = r.BootID(); e != nil {
		return s, e
	}
	if e = r.Conn.Object(raucService, raucPath).CallWithContext(ctx, raucInterface+".GetPrimary", 0).Store(&s.Primary); e != nil {
		return s, e
	}
	var slots []raucSlot
	if e = r.Conn.Object(raucService, raucPath).CallWithContext(ctx, raucInterface+".GetSlotStatus", 0).Store(&slots); e != nil {
		return s, e
	}
	for _, slot := range slots {
		p := slot.Properties
		class, _ := p["class"].Value().(string)
		bootname, _ := p["bootname"].Value().(string)
		if class != "rootfs" || bootname == "" {
			continue
		}
		state, _ := p["state"].Value().(string)
		if state == "booted" {
			s.Booted = slot.Name
		}
		version, _ := p["bundle.version"].Value().(string)
		boot, _ := p["boot-status"].Value().(string)
		s.Slots = append(s.Slots, Slot{Name: slot.Name, Version: version, Good: boot == "good"})
	}
	if s.Booted == "" {
		return s, errors.New("RAUC did not identify booted rootfs")
	}
	return s, nil
}
func (r *RAUC) Install(ctx context.Context, path string, release Release) error {
	// Subscribe before InstallBundle. Caller must never interpret context timeout as
	// cancellation: RAUC owns the ongoing flash operation independently of this process.
	opts := []dbus.MatchOption{dbus.WithMatchSender(raucService), dbus.WithMatchObjectPath(raucPath), dbus.WithMatchInterface(raucInterface), dbus.WithMatchMember("Completed")}
	if e := r.Conn.AddMatchSignal(opts...); e != nil {
		return e
	}
	defer r.Conn.RemoveMatchSignal(opts...)
	ch := make(chan *dbus.Signal, 16)
	r.Conn.Signal(ch)
	defer r.Conn.RemoveSignal(ch)
	s, e := r.Status(ctx)
	if e != nil {
		return e
	}
	if s.Operation != "idle" {
		return errors.New("RAUC busy")
	}
	if s.Compatible != release.Compatible {
		return errors.New("RAUC compatible mismatch")
	}
	if e = r.Conn.Object(raucService, raucPath).CallWithContext(ctx, raucInterface+".InstallBundle", 0, path, map[string]dbus.Variant{}).Err; e != nil {
		return e
	}
	for {
		select {
		case <-ctx.Done():
			return errors.New("RAUC completion unknown; backend may still be installing")
		case sig, ok := <-ch:
			if !ok {
				return errors.New("RAUC bus closed; installation outcome unknown")
			}
			if sig.Name != raucInterface+".Completed" || len(sig.Body) != 1 {
				continue
			}
			code, ok := sig.Body[0].(int32)
			if !ok {
				return errors.New("malformed RAUC completion")
			}
			if code != 0 {
				return fmt.Errorf("RAUC installation failed with code %d", code)
			}
			return nil
		}
	}
}
func (r *RAUC) Mark(ctx context.Context, state, slot string) error {
	if state != "good" && state != "bad" && state != "active" {
		return errors.New("invalid mark state")
	}
	var actual, message string
	if e := r.Conn.Object(raucService, raucPath).CallWithContext(ctx, raucInterface+".Mark", 0, state, slot).Store(&actual, &message); e != nil {
		return e
	}
	if actual != slot {
		return errors.New("RAUC marked an unexpected slot")
	}
	return nil
}
func (r *RAUC) Reboot(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return r.Conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").CallWithContext(c, "org.freedesktop.login1.Manager.Reboot", 0, false).Err
}
