// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memSpans struct {
	mu    sync.Mutex
	spans []Span
}

func (m *memSpans) Export(s Span) {
	m.mu.Lock()
	m.spans = append(m.spans, s)
	m.mu.Unlock()
}

func TestJobSpansShareOneTrace(t *testing.T) {
	h := newHarness(t)
	mem := &memSpans{}
	h.e.Spans = mem
	h.submit(t)
	h.until(t, Committed)
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if len(mem.spans) < 2 {
		t.Fatalf("spans=%d", len(mem.spans))
	}
	trace := mem.spans[0].TraceID
	if len(trace) != 32 {
		t.Fatal(trace)
	}
	sawCommit := false
	for i, s := range mem.spans {
		if s.TraceID != trace {
			t.Fatalf("trace split: %s", s.TraceID)
		}
		if len(s.SpanID) != 16 {
			t.Fatal(s.SpanID)
		}
		if i > 0 && s.ParentID != mem.spans[i-1].SpanID {
			t.Fatalf("parent %s want %s", s.ParentID, mem.spans[i-1].SpanID)
		}
		if s.Name == "ota.committed" && s.Attrs["job_id"] == "job-1" && !s.Error {
			sawCommit = true
		}
	}
	if !sawCommit {
		t.Fatal("missing commit span")
	}
}

func TestOTLPPostsHexTrace(t *testing.T) {
	var body []byte
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = b
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("collector-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Config{OTLPEndpoint: srv.URL, OTLPTokenFile: token}
	exp, err := NewOTLPExporter(c)
	if err != nil {
		t.Fatal(err)
	}
	exp.Export(Span{
		TraceID: "5b8aa5a2d2c872e8321cf37308d69df2",
		SpanID:  "051581bf3cb55c13",
		Name:    "ota.committed",
		Start:   time.Unix(0, 10),
		End:     time.Unix(0, 20),
		Attrs:   map[string]string{"job_id": "job-1", "state": "committed"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	exp.Shutdown(ctx)
	if auth != "Bearer collector-secret" {
		t.Fatal(auth)
	}
	var doc struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					TraceID string `json:"traceId"`
					Name    string `json:"name"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ResourceSpans[0].ScopeSpans[0].Spans[0].TraceID != "5b8aa5a2d2c872e8321cf37308d69df2" {
		t.Fatalf("%s", body)
	}
}

func TestOTLPEndpointRejectsPlainHTTP(t *testing.T) {
	c := Config{
		DeviceID: "device-1", Compatible: "test-board", StateDir: "/var/lib/zyvor-ota",
		Socket: "/run/zyvor-ota/agent.sock", Backend: "simulator",
		TrustKeys: map[string]string{"k": "AAAA"}, DownloadHosts: []string{"updates.example:443"},
		MaxArtifactBytes: 1 << 20, HealthTimeoutSeconds: 10, HealthStableSeconds: 0,
		OTLPEndpoint: "http://collector.example/v1/traces",
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "otlp") {
		t.Fatal(err)
	}
}
