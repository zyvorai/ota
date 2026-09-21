// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRestoresJournal(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	dest := filepath.Join(t.TempDir(), "copy.db")
	if err := h.s.Backup(dest); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Backup(dest); err == nil {
		t.Fatal("overwrote an existing backup")
	}
	if err := h.s.Backup("relative.db"); err == nil {
		t.Fatal("accepted a relative path")
	}
	dir := t.TempDir()
	if err := os.Rename(dest, filepath.Join(dir, "ota.db")); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, ok := restored.View().Jobs[h.assignment.JobID]; !ok {
		t.Fatal("backup lost the job")
	}
	var buf strings.Builder
	if err = h.s.ExportArchive(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Fatal(buf.String())
	}
}

func TestBackupAPIRejectsRelativePath(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/backup", strings.NewReader(`{"path":"relative.db"}`))
	w := httptest.NewRecorder()
	h.e.Handler().ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/archive", nil)
	w = httptest.NewRecorder()
	h.e.Handler().ServeHTTP(w, req)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("archive %d %s", w.Code, w.Body.String())
	}
}
