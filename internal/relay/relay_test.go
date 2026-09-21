// SPDX-License-Identifier: Apache-2.0
package relay

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOneHundredClientsShareOneUpstreamFetch(t *testing.T) {
	body := strings.Repeat("a", 128)
	var fetches int
	s := &Server{
		Dir:   t.TempDir(),
		Token: "peer-token",
		Upstream: func(sha string) (io.ReadCloser, error) {
			fetches++
			return io.NopCloser(strings.NewReader(body)), nil
		},
	}
	// Upstream digest must match the bytes. Compute by a first request through the handler
	// after we know the hash.
	sum := sha256Hex(body)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/artifacts/"+sum, nil)
			req.Header.Set("Authorization", "Bearer peer-token")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				errCh <- fmt.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if s.Fetches() != 1 {
		t.Fatalf("upstream fetches=%d", s.Fetches())
	}
	unauth, _ := http.Get(srv.URL + "/artifacts/" + sum)
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated peer status %d", unauth.StatusCode)
	}
	unauth.Body.Close()
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}
