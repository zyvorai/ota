// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAPIOverRealUnixSocket(t *testing.T) {
	h := newHarness(t)
	l, err := ListenUnix(h.e.Config.Socket)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skip("runtime prohibits Unix sockets; covered by CI on standard Linux")
		}
		t.Fatal(err)
	}
	server := http.Server{Handler: h.e.Handler()}
	go server.Serve(l)
	t.Cleanup(func() { server.Close() })
	client := UnixClient(h.e.Config.Socket)
	b, _ := json.Marshal(h.assignment)
	resp, err := client.Post("http://unix/v1/assignments", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatal(resp.StatusCode)
	}
	for _, path := range []string{"/v1/status", "/v1/jobs/job-1", "/v1/events", "/metrics"} {
		resp, err = client.Get("http://unix" + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(path, resp.StatusCode)
		}
	}
	st, _ := os.Stat(h.e.Config.Socket)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if _, err = ListenUnix(h.e.Config.Socket); err == nil {
		t.Fatal("live socket replaced")
	}
}
func TestAPIRejectsMalformedOversizedAndWrongMethod(t *testing.T) {
	h := newHarness(t)
	for _, body := range []string{"{", `{"unknown":true}`, strings.Repeat("x", 300<<10)} {
		req := httptest.NewRequest("POST", "/v1/assignments", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.e.Handler().ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.e.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/reboot", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
func TestSocketRefusesRegularFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.sock")
	_ = os.WriteFile(p, []byte("keep"), 0600)
	if _, e := ListenUnix(p); e == nil {
		t.Fatal("file replaced")
	}
}
func TestFleetTLSDeliveryAndAcknowledgment(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}
		if strings.HasSuffix(r.URL.Path, "/events") {
			var events []Event
			if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
				t.Error(err)
			}
			fmt.Fprintf(w, `{"sequence":%d}`, events[len(events)-1].Sequence)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	f := Fleet{Config: h.e.Config, Client: server.Client(), token: "secret"}
	f.Config.FleetURL = server.URL
	if err := f.Sync(context.Background(), h.e); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(h.s.View().Events) != 0 {
		t.Fatal("events not acknowledged")
	}
}
func TestFleetOutboxRetainedOnFailureOrInvalidAck(t *testing.T) {
	for _, kind := range []string{"error", "ack"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.submit(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "error" {
					w.WriteHeader(503)
				} else {
					io.WriteString(w, `{"sequence":999}`)
				}
			}))
			defer server.Close()
			f := Fleet{Config: h.e.Config, Client: server.Client()}
			f.Config.FleetURL = server.URL
			if err := f.Sync(context.Background(), h.e); err == nil {
				t.Fatal("bad response accepted")
			}
			if len(h.s.View().Events) != 1 {
				t.Fatal("events lost")
			}
		})
	}
}
func TestFleetReceivesSignedAssignment(t *testing.T) {
	h := newHarness(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(h.assignment) }))
	defer server.Close()
	f := Fleet{Config: h.e.Config, Client: server.Client()}
	f.Config.FleetURL = server.URL
	if err := f.Sync(context.Background(), h.e); err != nil {
		t.Fatal(err)
	}
	if h.state() != Accepted {
		t.Fatal(h.state())
	}
}
func TestFleetRejectsUntrustedTLS(t *testing.T) {
	h := newHarness(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	c := h.e.Config
	c.FleetURL = server.URL
	f, err := NewFleet(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(context.Background(), h.e); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}
