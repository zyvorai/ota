// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// This is a real private D-Bus daemon with a protocol test double for RAUC.
// It tests transport/serialization, not flash writes or bootloader correctness.
type mockRAUC struct {
	mu        sync.Mutex
	conn      *dbus.Conn
	operation string
	primary   string
	version   string
	result    int32
	hang      bool
	marked    string
	rebooted  bool
}

func (m *mockRAUC) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch name {
	case "Operation":
		return dbus.MakeVariant(m.operation), nil
	case "Compatible":
		return dbus.MakeVariant("test-board"), nil
	}
	return dbus.Variant{}, dbus.MakeFailedError(context.Canceled)
}
func (m *mockRAUC) GetPrimary() (string, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.primary, nil
}
func (m *mockRAUC) GetSlotStatus() ([]raucSlot, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []raucSlot{{"rootfs.0", map[string]dbus.Variant{"class": dbus.MakeVariant("rootfs"), "bootname": dbus.MakeVariant("A"), "state": dbus.MakeVariant("booted"), "boot-status": dbus.MakeVariant("good"), "bundle.version": dbus.MakeVariant("factory")}}, {"rootfs.1", map[string]dbus.Variant{"class": dbus.MakeVariant("rootfs"), "bootname": dbus.MakeVariant("B"), "state": dbus.MakeVariant("inactive"), "boot-status": dbus.MakeVariant("bad"), "bundle.version": dbus.MakeVariant(m.version)}}}, nil
}
func (m *mockRAUC) InstallBundle(path string, args map[string]dbus.Variant) *dbus.Error {
	m.mu.Lock()
	m.primary = "rootfs.1"
	m.version = "1.0.1"
	hang := m.hang
	result := m.result
	m.mu.Unlock()
	if !hang {
		go func() { _ = m.conn.Emit(raucPath, raucInterface+".Completed", result) }()
	}
	return nil
}
func (m *mockRAUC) Mark(state, slot string) (string, string, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.marked = state + ":" + slot
	return slot, "ok", nil
}
func (m *mockRAUC) Reboot(interactive bool) *dbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rebooted = true
	return nil
}
func testRAUC(t *testing.T) (*RAUC, *mockRAUC) {
	t.Helper()
	binary, e := exec.LookPath("dbus-daemon")
	if e != nil {
		t.Skip("dbus-daemon required for protocol integration test")
	}
	config := filepath.Join(t.TempDir(), "bus.conf")
	if e = os.WriteFile(config, []byte(`<busconfig><type>session</type><listen>tcp:host=127.0.0.1,port=0</listen><auth>ANONYMOUS</auth><allow_anonymous/><apparmor mode="disabled"/><policy context="default"><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>`), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, "--config-file="+config, "--nofork", "--print-address=1")
	cmd.Stderr = os.Stderr
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("no D-Bus address")
	}
	address := scanner.Text()
	server, e := dbus.Connect(address, dbus.WithAuth(dbus.AuthAnonymous()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { server.Close() })
	if _, e = server.RequestName(raucService, dbus.NameFlagDoNotQueue); e != nil {
		t.Fatal(e)
	}
	if _, e = server.RequestName("org.freedesktop.login1", dbus.NameFlagDoNotQueue); e != nil {
		t.Fatal(e)
	}
	mock := &mockRAUC{conn: server, operation: "idle", primary: "rootfs.0", version: "factory"}
	for _, x := range []struct {
		path  dbus.ObjectPath
		iface string
	}{{raucPath, raucInterface}, {raucPath, "org.freedesktop.DBus.Properties"}, {"/org/freedesktop/login1", "org.freedesktop.login1.Manager"}} {
		if e = server.Export(mock, x.path, x.iface); e != nil {
			t.Fatal(e)
		}
	}
	client, e := dbus.Connect(address, dbus.WithAuth(dbus.AuthAnonymous()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { client.Close() })
	return &RAUC{Conn: client, BootID: func() (string, error) { return "boot-test", nil }}, mock
}
func TestRAUCDBusStatusInstallMarkAndReboot(t *testing.T) {
	r, m := testRAUC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, e := r.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if s.Booted != "rootfs.0" || s.Primary != "rootfs.0" || s.Compatible != "test-board" {
		t.Fatal(s)
	}
	if e = r.Install(ctx, "/safe/test.raucb", Release{Compatible: "test-board"}); e != nil {
		t.Fatal(e)
	}
	if e = r.Mark(ctx, "good", "rootfs.1"); e != nil {
		t.Fatal(e)
	}
	if e = r.Reboot(ctx); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.marked != "good:rootfs.1" || !m.rebooted {
		t.Fatal("missing D-Bus action")
	}
}
func TestRAUCDBusFailureAndTimeout(t *testing.T) {
	for _, kind := range []string{"failure", "timeout", "busy", "compatible"} {
		t.Run(kind, func(t *testing.T) {
			r, m := testRAUC(t)
			release := Release{Compatible: "test-board"}
			switch kind {
			case "failure":
				m.result = 1
			case "timeout":
				m.hang = true
			case "busy":
				m.operation = "installing"
			case "compatible":
				release.Compatible = "other"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if e := r.Install(ctx, "/safe/test.raucb", release); e == nil {
				t.Fatal("unexpected success")
			}
		})
	}
}
