// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"time"
)

const Version = "0.1.0"

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Release struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	Sequence   uint64    `json:"sequence"`
	Compatible string    `json:"compatible"`
	Backend    string    `json:"backend"`
	Version    string    `json:"version"`
	Expires    time.Time `json:"expires"`
	Artifact   Artifact  `json:"artifact"`
	SBOMSHA256 string    `json:"sbom_sha256,omitempty"`
}

// Payload contains base64-encoded exact JSON bytes; signatures never depend on reserialization.
type Envelope struct {
	KeyID     string `json:"key_id"`
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}
type Assignment struct {
	JobID      string    `json:"job_id"`
	DeviceID   string    `json:"device_id"`
	Release    Envelope  `json:"release"`
	NotBefore  time.Time `json:"not_before"`
	Deadline   time.Time `json:"deadline"`
	AutoReboot bool      `json:"auto_reboot"`
}
type Check struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}
type Config struct {
	DeviceID             string            `json:"device_id"`
	Compatible           string            `json:"compatible"`
	StateDir             string            `json:"state_dir"`
	Socket               string            `json:"socket"`
	Backend              string            `json:"backend"`
	AllowDeviceWrites    bool              `json:"allow_device_writes"`
	TrustKeys            map[string]string `json:"trust_keys"`
	DownloadHosts        []string          `json:"download_hosts"`
	MaxArtifactBytes     int64             `json:"max_artifact_bytes"`
	ReserveBytes         uint64            `json:"reserve_bytes"`
	HealthTimeoutSeconds int               `json:"health_timeout_seconds"`
	HealthStableSeconds  int               `json:"health_stable_seconds"`
	Checks               []Check           `json:"checks"`
	FleetURL             string            `json:"fleet_url,omitempty"`
	FleetTokenFile       string            `json:"fleet_token_file,omitempty"`
	FleetCA              string            `json:"fleet_ca,omitempty"`
	FleetCert            string            `json:"fleet_cert,omitempty"`
	FleetKey             string            `json:"fleet_key,omitempty"`
}

func (c Config) Validate() error {
	if !identifier.MatchString(c.DeviceID) || !identifier.MatchString(c.Compatible) {
		return errors.New("invalid device_id or compatible")
	}
	if !filepath.IsAbs(c.StateDir) || !filepath.IsAbs(c.Socket) {
		return errors.New("state_dir and socket must be absolute")
	}
	if c.Backend != "simulator" && c.Backend != "rauc" {
		return errors.New("supported backends: simulator, rauc")
	}
	if c.Backend == "rauc" && (!c.AllowDeviceWrites || len(c.Checks) == 0) {
		return errors.New("RAUC requires allow_device_writes and local health checks")
	}
	if len(c.TrustKeys) == 0 || len(c.DownloadHosts) == 0 {
		return errors.New("trust_keys and download_hosts are required")
	}
	if c.MaxArtifactBytes <= 0 || c.MaxArtifactBytes > 1<<40 {
		return errors.New("max_artifact_bytes must be 1..1TiB")
	}
	if c.ReserveBytes > 1<<50 {
		return errors.New("reserve_bytes exceeds supported limit")
	}
	if c.HealthTimeoutSeconds < 1 || c.HealthTimeoutSeconds > 3600 || c.HealthStableSeconds < 0 || c.HealthStableSeconds >= c.HealthTimeoutSeconds {
		return errors.New("invalid health timing")
	}
	for _, check := range c.Checks {
		if check.Kind == "file" && filepath.IsAbs(check.Target) {
			continue
		}
		if check.Kind == "systemd" && identifier.MatchString(check.Target) {
			continue
		}
		if check.Kind == "http" {
			u, e := url.Parse(check.Target)
			if e == nil && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") && u.User == nil {
				continue
			}
		}
		return fmt.Errorf("invalid health check: %s", check.Kind)
	}
	if c.FleetURL != "" {
		u, e := url.Parse(c.FleetURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("fleet_url must be a plain HTTPS base URL")
		}
		if c.FleetTokenFile == "" && (c.FleetCert == "" || c.FleetKey == "") {
			return errors.New("Fleet requires a token file or mTLS")
		}
	}
	return nil
}
func StrictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

type State string

const (
	Accepted        State = "accepted"
	Downloading     State = "downloading"
	Verified        State = "verified"
	Installing      State = "installing"
	AwaitingReboot  State = "awaiting_reboot"
	CheckingHealth  State = "checking_health"
	Committing      State = "committing"
	Committed       State = "committed"
	RollbackPending State = "rollback_pending"
	RolledBack      State = "rolled_back"
	Failed          State = "failed"
	NeedsRecovery   State = "needs_recovery"
)

func Terminal(s State) bool { return s == Committed || s == RolledBack || s == Failed }

type Job struct {
	Assignment      Assignment `json:"assignment"`
	Release         Release    `json:"release"`
	Fingerprint     string     `json:"fingerprint"`
	State           State      `json:"state"`
	Error           string     `json:"error,omitempty"`
	OldSlot         string     `json:"old_slot,omitempty"`
	OldVersion      string     `json:"old_version,omitempty"`
	TargetSlot      string     `json:"target_slot,omitempty"`
	BootID          string     `json:"boot_id,omitempty"`
	HealthStarted   time.Time  `json:"health_started,omitempty"`
	RebootRequested bool       `json:"reboot_requested"`
	Updated         time.Time  `json:"updated"`
}
type Event struct {
	Sequence uint64    `json:"sequence"`
	JobID    string    `json:"job_id"`
	State    State     `json:"state"`
	Error    string    `json:"error,omitempty"`
	Time     time.Time `json:"time"`
}
type Database struct {
	Schema        int            `json:"schema"`
	HighSequence  uint64         `json:"high_sequence"`
	EventSequence uint64         `json:"event_sequence"`
	Jobs          map[string]Job `json:"jobs"`
	Active        string         `json:"active,omitempty"`
	Events        []Event        `json:"events"`
	BootCheck     BootCheck      `json:"boot_check"`
}
type BootCheck struct {
	BootID    string    `json:"boot_id"`
	Started   time.Time `json:"started"`
	Confirmed bool      `json:"confirmed"`
}
