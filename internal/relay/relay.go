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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Dir         string
	Token       string
	Upstream    func(sha string) (io.ReadCloser, error)
	OriginHosts []string
	mu          sync.Mutex
	fill        sync.Mutex
	fetches     int
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
			body, err := s.origin(r)
			if err != nil {
				http.Error(w, "missing", http.StatusNotFound)
				return
			}
			defer body.Close()
			s.noteFetch()
			if err = s.store(path, sum, body); err != nil {
				http.Error(w, "upstream", http.StatusBadGateway)
				return
			}
			http.ServeFile(w, r, path)
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

func (s *Server) noteFetch() {
	s.mu.Lock()
	s.fetches++
	s.mu.Unlock()
}

func (s *Server) store(path, sum string, body io.Reader) error {
	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(f, io.TeeReader(body, h))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		os.Remove(tmp)
		return errors.New("digest")
	}
	return os.Rename(tmp, path)
}

func (s *Server) origin(r *http.Request) (io.ReadCloser, error) {
	raw := strings.TrimSpace(r.Header.Get("X-Zyvor-Artifact-Url"))
	if raw == "" || len(s.OriginHosts) == 0 {
		return nil, ErrNoUpstream
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrNoUpstream
	}
	local := u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if u.Scheme != "https" && !local {
		return nil, ErrNoUpstream
	}
	if !hostAllowed(s.OriginHosts, u.Host) {
		return nil, ErrNoUpstream
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirect")
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, errors.New("origin status")
	}
	return resp.Body, nil
}

func hostAllowed(hosts []string, host string) bool {
	for _, h := range hosts {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return true
		}
	}
	return false
}

// ErrNoUpstream is returned by tests that should have hit the cache.
var ErrNoUpstream = errors.New("upstream disabled")
