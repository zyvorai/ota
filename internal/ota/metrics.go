// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Stats are process-local counters. They reset when the agent restarts.
type Stats struct {
	mu              sync.Mutex
	stateSeconds    map[State]float64
	stateEntered    time.Time
	currentState    State
	downloadBytes   int64
	downloadNanos   int64
	downloadResumes int
	healthFailures  map[string]int
	commits         int
	rollbacks       int
	recoveries      int
	fleetSyncOK     int
	fleetSyncFail   int
	fleetSyncNanos  int64
	lastSuccess     time.Time
	slot            string
	release         string
}

func (s *Stats) observeState(now time.Time, next State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stateSeconds == nil {
		s.stateSeconds = map[State]float64{}
	}
	if !s.stateEntered.IsZero() && s.currentState != "" {
		s.stateSeconds[s.currentState] += now.Sub(s.stateEntered).Seconds()
	}
	s.currentState = next
	s.stateEntered = now
	switch next {
	case Committed:
		s.commits++
		s.lastSuccess = now
	case RolledBack:
		s.rollbacks++
	case NeedsRecovery:
		s.recoveries++
	}
}

func (s *Stats) addDownload(n int64, resumed bool, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloadBytes += n
	s.downloadNanos += elapsed.Nanoseconds()
	if resumed {
		s.downloadResumes++
	}
}

func (s *Stats) healthFailure(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.healthFailures == nil {
		s.healthFailures = map[string]int{}
	}
	s.healthFailures[name]++
}

func (s *Stats) fleetSync(ok bool, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fleetSyncNanos += elapsed.Nanoseconds()
	if ok {
		s.fleetSyncOK++
	} else {
		s.fleetSyncFail++
	}
}

func (s *Stats) setSlot(slot, release string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slot = slot
	s.release = release
}

func (e *Engine) WriteMetrics(w io.Writer, pending int, sequence uint64) {
	e.Stats.mu.Lock()
	defer e.Stats.mu.Unlock()
	fmt.Fprintf(w, "# TYPE zyvor_ota_pending_events gauge\nzyvor_ota_pending_events %d\n", pending)
	fmt.Fprintf(w, "# TYPE zyvor_ota_release_sequence gauge\nzyvor_ota_release_sequence %d\n", sequence)
	fmt.Fprintf(w, "# TYPE zyvor_ota_state_seconds_total counter\n")
	for state, sec := range e.Stats.stateSeconds {
		fmt.Fprintf(w, "zyvor_ota_state_seconds_total{state=%q} %g\n", state, sec)
	}
	rate := 0.0
	if e.Stats.downloadNanos > 0 {
		rate = float64(e.Stats.downloadBytes) / (float64(e.Stats.downloadNanos) / 1e9)
	}
	fmt.Fprintf(w, "# TYPE zyvor_ota_download_bytes_total counter\nzyvor_ota_download_bytes_total %d\n", e.Stats.downloadBytes)
	fmt.Fprintf(w, "# TYPE zyvor_ota_download_bytes_per_second gauge\nzyvor_ota_download_bytes_per_second %g\n", rate)
	fmt.Fprintf(w, "# TYPE zyvor_ota_download_resumes_total counter\nzyvor_ota_download_resumes_total %d\n", e.Stats.downloadResumes)
	fmt.Fprintf(w, "# TYPE zyvor_ota_health_failures_total counter\n")
	for name, n := range e.Stats.healthFailures {
		fmt.Fprintf(w, "zyvor_ota_health_failures_total{check=%q} %d\n", name, n)
	}
	fmt.Fprintf(w, "# TYPE zyvor_ota_commits_total counter\nzyvor_ota_commits_total %d\n", e.Stats.commits)
	fmt.Fprintf(w, "# TYPE zyvor_ota_rollbacks_total counter\nzyvor_ota_rollbacks_total %d\n", e.Stats.rollbacks)
	fmt.Fprintf(w, "# TYPE zyvor_ota_recoveries_total counter\nzyvor_ota_recoveries_total %d\n", e.Stats.recoveries)
	fmt.Fprintf(w, "# TYPE zyvor_ota_fleet_sync_total counter\nzyvor_ota_fleet_sync_total{result=\"ok\"} %d\nzyvor_ota_fleet_sync_total{result=\"error\"} %d\n", e.Stats.fleetSyncOK, e.Stats.fleetSyncFail)
	lat := 0.0
	if e.Stats.fleetSyncOK+e.Stats.fleetSyncFail > 0 {
		lat = float64(e.Stats.fleetSyncNanos) / float64(e.Stats.fleetSyncOK+e.Stats.fleetSyncFail) / 1e9
	}
	fmt.Fprintf(w, "# TYPE zyvor_ota_fleet_sync_seconds gauge\nzyvor_ota_fleet_sync_seconds %g\n", lat)
	fmt.Fprintf(w, "# TYPE zyvor_ota_cache_bytes gauge\nzyvor_ota_cache_bytes %d\n", cacheBytes(e.Config.StateDir))
	fmt.Fprintf(w, "# TYPE zyvor_ota_current_slot info\nzyvor_ota_current_slot{slot=%q,release=%q} 1\n", e.Stats.slot, e.Stats.release)
	since := -1.0
	if !e.Stats.lastSuccess.IsZero() {
		since = e.Now().Sub(e.Stats.lastSuccess).Seconds()
	}
	fmt.Fprintf(w, "# TYPE zyvor_ota_seconds_since_success gauge\nzyvor_ota_seconds_since_success %g\n", since)
	_ = strconv.Itoa(0)
}

func cacheBytes(dir string) int64 {
	var n int64
	_ = filepath.Walk(filepath.Join(dir, "cache"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}
