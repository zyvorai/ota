// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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

// Target is one typed payload inside a schema-2 release. Handlers are fixed;
// the agent does not run release scripts.
type Target struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Artifact     Artifact `json:"artifact"`
	Reboot       string   `json:"reboot,omitempty"`
	Health       string   `json:"health,omitempty"`
	Requires     string   `json:"requires,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Rollback     string   `json:"rollback,omitempty"`
	StorageBytes int64    `json:"storage_bytes,omitempty"`
}

type Release struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	Sequence   uint64    `json:"sequence"`
	Compatible string    `json:"compatible"`
	Backend    string    `json:"backend"`
	Version    string    `json:"version"`
	Expires    time.Time `json:"expires"`
	Artifact   Artifact  `json:"artifact,omitempty"`
	SBOMSHA256 string    `json:"sbom_sha256,omitempty"`
	Targets    []Target  `json:"targets,omitempty"`
	// Adaptive is refused unless the device config explicitly allows the
	// board's existing RAUC adaptive mode. This agent does not invent a delta format.
	Adaptive bool `json:"adaptive,omitempty"`
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
	CampaignID string    `json:"campaign_id,omitempty"`
}
type Check struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Name   string `json:"name,omitempty"`
}
type Config struct {
	DeviceID              string            `json:"device_id"`
	Compatible            string            `json:"compatible"`
	StateDir              string            `json:"state_dir"`
	Socket                string            `json:"socket"`
	Backend               string            `json:"backend"`
	AllowDeviceWrites     bool              `json:"allow_device_writes"`
	TrustKeys             map[string]string `json:"trust_keys"`
	DownloadHosts         []string          `json:"download_hosts"`
	MaxArtifactBytes      int64             `json:"max_artifact_bytes"`
	ReserveBytes          uint64            `json:"reserve_bytes"`
	HealthTimeoutSeconds  int               `json:"health_timeout_seconds"`
	HealthStableSeconds   int               `json:"health_stable_seconds"`
	Checks                []Check           `json:"checks"`
	Capabilities          []string          `json:"capabilities,omitempty"`
	BandwidthBytesPerSec  int64             `json:"bandwidth_bytes_per_sec,omitempty"`
	DownloadJitterSeconds int               `json:"download_jitter_seconds,omitempty"`
	DownloadWindowStart   string            `json:"download_window_start,omitempty"`
	DownloadWindowEnd     string            `json:"download_window_end,omitempty"`
	LocalMediaDir         string            `json:"local_media_dir,omitempty"`
	AllowAdaptive         bool              `json:"allow_adaptive,omitempty"`
	OTLPEndpoint          string            `json:"otlp_endpoint,omitempty"`
	OTLPTokenFile         string            `json:"otlp_token_file,omitempty"`
	RelayURL              string            `json:"relay_url,omitempty"`
	RelayTokenFile        string            `json:"relay_token_file,omitempty"`
	FleetURL              string            `json:"fleet_url,omitempty"`
	FleetTokenFile        string            `json:"fleet_token_file,omitempty"`
	FleetCA               string            `json:"fleet_ca,omitempty"`
	FleetCert             string            `json:"fleet_cert,omitempty"`
	FleetKey              string            `json:"fleet_key,omitempty"`
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
	if c.BandwidthBytesPerSec < 0 {
		return errors.New("bandwidth_bytes_per_sec must be >= 0")
	}
	if c.DownloadJitterSeconds < 0 || c.DownloadJitterSeconds > 3600 {
		return errors.New("download_jitter_seconds must be 0..3600")
	}
	if (c.DownloadWindowStart == "") != (c.DownloadWindowEnd == "") {
		return errors.New("download window needs both start and end")
	}
	if c.DownloadWindowStart != "" {
		if _, _, err := parseClock(c.DownloadWindowStart); err != nil {
			return err
		}
		if _, _, err := parseClock(c.DownloadWindowEnd); err != nil {
			return err
		}
	}
	if c.LocalMediaDir != "" && !filepath.IsAbs(c.LocalMediaDir) {
		return errors.New("local_media_dir must be absolute")
	}
	if err := c.validateOTLP(); err != nil {
		return err
	}
	if err := c.validateRelay(); err != nil {
		return err
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

// ErrDeferred means the download is intentionally waiting, not failed.
var ErrDeferred = errors.New("deferred: outside download window")

// ErrJitter means this device is waiting out its share of the fleet start spread.
var ErrJitter = errors.New("deferred: download jitter")

// DownloadJitter spreads fleet download starts. Zero disables it.
// The offset is stable for a device_id so a retry does not roll a new delay.
func (c Config) DownloadJitter() time.Duration {
	if c.DownloadJitterSeconds <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(c.DeviceID))
	sec := time.Duration(h.Sum32() % uint32(c.DownloadJitterSeconds+1))
	return sec * time.Second
}

func (c Config) DownloadAllowed(now time.Time) error {
	if c.DownloadWindowStart == "" {
		return nil
	}
	sh, sm, err := parseClock(c.DownloadWindowStart)
	if err != nil {
		return err
	}
	eh, em, err := parseClock(c.DownloadWindowEnd)
	if err != nil {
		return err
	}
	cur := now.Hour()*60 + now.Minute()
	start := sh*60 + sm
	end := eh*60 + em
	inside := false
	if start <= end {
		inside = cur >= start && cur < end
	} else {
		inside = cur >= start || cur < end
	}
	if !inside {
		return ErrDeferred
	}
	return nil
}

func parseClock(v string) (int, int, error) {
	var h, m int
	if _, err := fmt.Sscanf(v, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 || len(v) != 5 {
		return 0, 0, errors.New("download window must be HH:MM")
	}
	return h, m, nil
}

func (r Release) OSTarget() (Target, bool) {
	for _, t := range r.Targets {
		if t.Type == "os.rauc" {
			return t, true
		}
	}
	return Target{}, false
}

func (r Release) HasOS() bool {
	if _, ok := r.OSTarget(); ok {
		return true
	}
	return r.Schema == 1
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
	Deferred        bool       `json:"deferred,omitempty"`
	DownloadAfter   time.Time  `json:"download_after,omitempty"`
	PayloadsApplied bool       `json:"payloads_applied,omitempty"`
	TraceID         string     `json:"trace_id,omitempty"`
	SpanID          string     `json:"span_id,omitempty"`
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
