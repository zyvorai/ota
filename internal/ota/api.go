// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func JSONResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func decodeRequest(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		return e
	}
	return StrictJSON(b, v)
}
func (e *Engine) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		d := e.Store.View()
		var j *Job
		if v, ok := d.Jobs[d.Active]; ok {
			j = &v
		}
		JSONResponse(w, 200, map[string]any{"version": Version, "device_id": e.Config.DeviceID, "backend": e.Config.Backend, "active": j, "high_sequence": d.HighSequence, "event_sequence": d.EventSequence, "pending_events": len(d.Events)})
	})
	mux.HandleFunc("GET /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, ok := e.Store.View().Jobs[r.PathValue("id")]
		if !ok {
			JSONResponse(w, 404, map[string]string{"error": "job not found"})
			return
		}
		JSONResponse(w, 200, j)
	})
	mux.HandleFunc("POST /v1/assignments", func(w http.ResponseWriter, r *http.Request) {
		var a Assignment
		if err := decodeRequest(w, r, &a); err != nil {
			JSONResponse(w, 400, map[string]string{"error": "invalid assignment JSON"})
			return
		}
		j, err := e.Submit(a)
		if err != nil {
			JSONResponse(w, 409, map[string]string{"error": err.Error()})
			return
		}
		JSONResponse(w, 202, j)
	})
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) { JSONResponse(w, 200, e.Store.View().Events) })
	mux.HandleFunc("POST /v1/events/ack", func(w http.ResponseWriter, r *http.Request) {
		var a struct {
			Sequence uint64 `json:"sequence"`
		}
		if err := decodeRequest(w, r, &a); err != nil {
			JSONResponse(w, 400, map[string]string{"error": "invalid ack"})
			return
		}
		apiAction(w, e.Ack(a.Sequence))
	})
	mux.HandleFunc("POST /v1/reboot", func(w http.ResponseWriter, r *http.Request) { apiAction(w, e.RequestReboot(r.Context())) })
	mux.HandleFunc("POST /v1/recover-abort", func(w http.ResponseWriter, r *http.Request) { apiAction(w, e.RecoverAbort(r.Context())) })
	mux.HandleFunc("POST /v1/gc", func(w http.ResponseWriter, r *http.Request) { apiAction(w, e.GC()) })
	mux.HandleFunc("POST /v1/backup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		if err := decodeRequest(w, r, &in); err != nil {
			JSONResponse(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if !filepath.IsAbs(in.Path) {
			JSONResponse(w, 400, map[string]string{"error": "backup destination must be absolute"})
			return
		}
		apiAction(w, e.Store.Backup(in.Path))
	})
	mux.HandleFunc("GET /v1/archive", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		if err := e.Store.ExportArchive(&buf); err != nil {
			JSONResponse(w, 409, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		d := e.Store.View()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		e.WriteMetrics(w, len(d.Events), d.HighSequence)
	})
	return mux
}
func apiAction(w http.ResponseWriter, err error) {
	if err != nil {
		JSONResponse(w, 409, map[string]string{"error": err.Error()})
		return
	}
	JSONResponse(w, 200, map[string]bool{"ok": true})
}
func ListenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	st, err := os.Lstat(path)
	if err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace non-socket path")
		}
		conn, dialErr := net.DialTimeout("unix", path, time.Second)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("socket already in use")
		}
		if err = os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
func UnixClient(path string) *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}
}
