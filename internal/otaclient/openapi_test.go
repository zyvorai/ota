// SPDX-License-Identifier: Apache-2.0
package otaclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAPIErrorUsesErrorField(t *testing.T) {
	err := &APIError{Status: 409, Body: "{\"error\":\"engine busy\"}\n"}
	if got := err.Error(); got != "ota api 409: engine busy" {
		t.Fatal(got)
	}
}

func TestOpenAPIErrorResponsesAreTyped(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	client, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "operationId:") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(trimmed, "operationId:"))
		if !strings.Contains(string(client), "operationId: "+id) {
			t.Fatalf("client missing operationId %s", id)
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "'400':" && trimmed != "'404':" && trimmed != "'409':" {
			continue
		}
		window := strings.Join(lines[i:min(i+12, len(lines))], "\n")
		if !strings.Contains(window, "#/components/schemas/Error") {
			t.Fatalf("%s at line %d has no Error schema", trimmed, i+1)
		}
	}
}

func TestClientBackupAndArchive(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/backup":
			if r.Method != http.MethodPost {
				t.Fatalf("method %s", r.Method)
			}
			var body struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			gotPath = body.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`+"\n")
		case "/v1/archive":
			if r.Method != http.MethodGet {
				t.Fatalf("method %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "[]\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Base: srv.URL}
	if err := c.Backup(context.Background(), "/var/backups/ota.db"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/var/backups/ota.db" {
		t.Fatal(gotPath)
	}
	raw, err := c.Archive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]\n" && string(raw) != "[]" {
		t.Fatal(string(raw))
	}
}
