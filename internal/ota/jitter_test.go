// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"testing"
	"time"
)

func TestDownloadJitterIsStablePerDevice(t *testing.T) {
	c := Config{DeviceID: "device-1", DownloadJitterSeconds: 30}
	if c.DownloadJitter() == 0 {
		t.Fatal("device-1 hashed to no delay; pick another id for the engine test")
	}
	if c.DownloadJitter() != c.DownloadJitter() {
		t.Fatal("jitter changed")
	}
	if got := (Config{DeviceID: "device-1"}).DownloadJitter(); got != 0 {
		t.Fatal(got)
	}
	off := Config{DeviceID: "other-device", DownloadJitterSeconds: 30}.DownloadJitter()
	if off < 0 || off > 30*time.Second {
		t.Fatal(off)
	}
}

func TestDownloadJitterDefersThenFetches(t *testing.T) {
	h := newHarness(t)
	h.e.Config.DownloadJitterSeconds = 30
	delay := h.e.Config.DownloadJitter()
	if delay == 0 {
		t.Fatal(delay)
	}
	h.submit(t)
	h.step(t)
	if h.state() != Downloading {
		t.Fatal(h.state())
	}
	job := h.s.View().Jobs[h.assignment.JobID]
	if !job.DownloadAfter.Equal(h.now.Add(delay)) {
		t.Fatalf("download_after %s", job.DownloadAfter)
	}
	h.step(t)
	job = h.s.View().Jobs[h.assignment.JobID]
	if h.state() != Downloading || !job.Deferred || job.Error != ErrJitter.Error() {
		t.Fatalf("state %s deferred %v err %s", h.state(), job.Deferred, job.Error)
	}
	h.now = job.DownloadAfter
	h.until(t, Committed)
}
