// SPDX-License-Identifier: Apache-2.0
// Package relay is a per-site artifact cache. Peers must present the shared token.
// It does not sign releases and does not bypass host allowlists on the agent.
package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Server struct {
	Dir      string
	Token    string
	Upstream func(sha string) (io.ReadCloser, error)
	mu       sync.Mutex
	fill     sync.Mutex
	fetches  int
}

func (s *Server) Fetches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /artifacts/{sha256}", s.get)
	return mux
}

func (s *Server) authorize(r *http.Request) bool {
	if s.Token == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return got == s.Token
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sum := r.PathValue("sha256")
	if len(sum) != 64 {
		http.Error(w, "bad digest", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.Dir, sum)
	if _, err := os.Stat(path); err != nil {
		s.fill.Lock()
		defer s.fill.Unlock()
		if _, err2 := os.Stat(path); err2 == nil {
			http.ServeFile(w, r, path)
			return
		}
		if s.Upstream == nil {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		body, err := s.Upstream(sum)
		if err != nil {
			http.Error(w, "upstream", http.StatusBadGateway)
			return
		}
		defer body.Close()
		s.mu.Lock()
		s.fetches++
		s.mu.Unlock()
		tmp := path + ".part"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			http.Error(w, "cache", http.StatusInternalServerError)
			return
		}
		h := sha256.New()
		if _, err = io.Copy(f, io.TeeReader(body, h)); err != nil {
			f.Close()
			http.Error(w, "cache", http.StatusBadGateway)
			return
		}
		f.Close()
		if hex.EncodeToString(h.Sum(nil)) != sum {
			os.Remove(tmp)
			http.Error(w, "digest", http.StatusBadGateway)
			return
		}
		if err = os.Rename(tmp, path); err != nil {
			http.Error(w, "cache", http.StatusInternalServerError)
			return
		}
	}
	http.ServeFile(w, r, path)
}

// ErrNoUpstream is returned by tests that should have hit the cache.
var ErrNoUpstream = errors.New("upstream disabled")
