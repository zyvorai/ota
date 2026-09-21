// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestPowerLossLeavesAConsistentSnapshot(t *testing.T) {
	if mode := os.Getenv("OTA_POWER_LOSS"); mode != "" {
		crashDuringSnapshot(mode)
		return
	}
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestPowerLossLeavesAConsistentSnapshot$", "-test.count=1")
			cmd.Env = append(os.Environ(), "OTA_POWER_LOSS="+mode, "OTA_STATE="+dir)
			err := cmd.Run()
			code := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			wantCode := 97
			wantSeq := uint64(7)
			if mode == "after" {
				wantCode = 98
				wantSeq = 8
			}
			if code != wantCode {
				t.Fatalf("exit %d, want %d", code, wantCode)
			}
			s, err := OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if got := s.View().HighSequence; got != wantSeq {
				t.Fatalf("high sequence %d, want %d", got, wantSeq)
			}
		})
	}
}

func crashDuringSnapshot(mode string) {
	dir := os.Getenv("OTA_STATE")
	s, err := OpenStore(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err = s.Update(func(d *Database) error {
		d.HighSequence = 7
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	switch mode {
	case "before":
		s.beforeCommit = func() { os.Exit(97) }
	case "after":
		s.afterCommit = func() { os.Exit(98) }
	default:
		os.Exit(2)
	}
	_ = s.Update(func(d *Database) error {
		d.HighSequence = 8
		return nil
	})
	os.Exit(0)
}
