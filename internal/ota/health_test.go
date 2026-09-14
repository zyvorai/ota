// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHealthFileAndHTTP(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "healthy")
	if err := os.WriteFile(ok, nil, 0644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)

	fn := LocalHealth([]Check{
		{Kind: "file", Target: ok},
		{Kind: "http", Target: srv.URL + "/healthz"},
	})
	if err := fn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := LocalHealth([]Check{{Kind: "file", Target: filepath.Join(dir, "missing")}})(context.Background()); err == nil {
		t.Fatal("missing file accepted")
	}
	if err := LocalHealth([]Check{{Kind: "http", Target: srv.URL + "/nope"}})(context.Background()); err == nil {
		t.Fatal("non-200 accepted")
	}
	if err := LocalHealth([]Check{{Kind: "mystery", Target: "x"}})(context.Background()); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestLocalHealthSystemdProbe(t *testing.T) {
	err := LocalHealth([]Check{{Kind: "systemd", Target: "zyvor-device-agent.service"}})(context.Background())
	if err == nil {
		t.Log("systemd reported active (unusual in unit-test hosts)")
	}
}
