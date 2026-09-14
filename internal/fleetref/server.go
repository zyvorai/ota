// SPDX-License-Identifier: Apache-2.0
// Package fleetref implements the proposed Fleet adapter contract (docs/FLEET.md)
// for lab integration tests and local bring-up. It is not a production fleet
// scheduler or Zyvor Fleet product deployment.
package fleetref

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/zyvorai/ota/internal/ota"
)

// Server is an in-memory Fleet contract implementation.
type Server struct {
	mu sync.Mutex
	// tokens maps device_id -> bearer token. Empty token means mTLS-only lab use
	// (authorization still requires a registered device id).
	tokens      map[string]string
	assignments map[string]*ota.Assignment
	// highest contiguous acknowledged sequence per device
	acked  map[string]uint64
	events map[string]map[uint64]ota.Event
}

func New() *Server {
	return &Server{
		tokens:      map[string]string{},
		assignments: map[string]*ota.Assignment{},
		acked:       map[string]uint64{},
		events:      map[string]map[uint64]ota.Event{},
	}
}

// RegisterDevice binds a device ID to an auth token. The server never authorizes
// solely because a device ID appears in the URL path.
func (s *Server) RegisterDevice(deviceID, token string) error {
	if deviceID == "" || token == "" {
		return errors.New("device id and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[deviceID] = token
	if s.events[deviceID] == nil {
		s.events[deviceID] = map[uint64]ota.Event{}
	}
	return nil
}

// SetAssignment stores the next assignment for a device (nil clears → 204).
func (s *Server) SetAssignment(deviceID string, a *ota.Assignment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a == nil {
		delete(s.assignments, deviceID)
		return
	}
	cp := *a
	s.assignments[deviceID] = &cp
}

// AckedSequence returns the highest contiguous sequence durably accepted.
func (s *Server) AckedSequence(deviceID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acked[deviceID]
}

func (s *Server) authorize(r *http.Request, deviceID string) bool {
	s.mu.Lock()
	want, ok := s.tokens[deviceID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	got = strings.TrimSpace(got)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/devices/{device_id}/assignment", s.getAssignment)
	mux.HandleFunc("POST /v1/devices/{device_id}/events", s.postEvents)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

func (s *Server) getAssignment(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	if !s.authorize(r, deviceID) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	a := s.assignments[deviceID]
	s.mu.Unlock()
	if a == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a)
}

func (s *Server) postEvents(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	if !s.authorize(r, deviceID) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var events []ota.Event
	if err := ota.StrictJSON(body, &events); err != nil {
		http.Error(w, "invalid events", http.StatusBadRequest)
		return
	}
	if len(events) == 0 || len(events) > 100 {
		http.Error(w, "event batch size", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events[deviceID] == nil {
		s.events[deviceID] = map[uint64]ota.Event{}
	}
	store := s.events[deviceID]
	for _, ev := range events {
		if prev, exists := store[ev.Sequence]; exists {
			// Idempotent insert: identical sequence must not change outcome.
			if prev.JobID != ev.JobID || prev.State != ev.State {
				http.Error(w, "conflicting event", http.StatusConflict)
				return
			}
			continue
		}
		store[ev.Sequence] = ev
	}
	// Highest contiguous from 1..n relative to prior ACK (sequences are 1-based in practice;
	// advance while next = acked+1 exists).
	acked := s.acked[deviceID]
	for {
		next := acked + 1
		if _, ok := store[next]; !ok {
			break
		}
		acked = next
	}
	// Never ACK above the last event in this request.
	last := events[len(events)-1].Sequence
	if acked > last {
		acked = last
	}
	s.acked[deviceID] = acked
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]uint64{"sequence": acked})
}
