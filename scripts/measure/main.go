// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zyvorai/ota/internal/ota"
	"github.com/zyvorai/ota/internal/relay"
)

const artifactBytes = 1 << 20

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peak := &rssPeak{}
	go peak.watch(ctx)
	before := rssKB()

	peak.reset()
	install, err := measureInstall()
	if err != nil {
		return err
	}
	install["rss_kb_peak"] = peak.max()
	peak.reset()
	rollback, err := measureRollback()
	if err != nil {
		return err
	}
	rollback["rss_kb_peak"] = peak.max()
	peak.reset()
	recovery, err := measureRecoverAbort()
	if err != nil {
		return err
	}
	recovery["rss_kb_peak"] = peak.max()
	peak.reset()
	wan, err := measureRelay()
	if err != nil {
		return err
	}
	wan["rss_kb_peak"] = peak.max()
	cancel()
	report := map[string]any{
		"measured_at": time.Now().UTC().Format(time.RFC3339),
		"environment": environment(),
		"rss_kb": map[string]int{
			"process_start": before,
		},
		"install":       install,
		"rollback":      rollback,
		"recover_abort": recovery,
		"relay":         wan,
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func environment() map[string]any {
	host, _ := exec.Command("uname", "-smr").Output()
	qemu := strings.TrimSpace(os.Getenv("QUALIFY_QEMU_IMAGE"))
	qemuNote := "not measured; QUALIFY_QEMU_IMAGE is unset or not a file"
	if qemu != "" {
		if st, err := os.Stat(qemu); err == nil && st.Mode().IsRegular() {
			qemuNote = "image file is present; this command does not boot QEMU"
		}
	}
	return map[string]any{
		"go":       runtime.Version(),
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"uname":    strings.TrimSpace(string(host)),
		"profile":  "simulator",
		"qemu":     qemuNote,
		"minewing": "not measured; no board run",
		"note":     "In-process simulator on this host. Install time is engine and localhost download time, not RAUC flash time.",
	}
}

func measureInstall() (map[string]any, error) {
	dir, err := os.MkdirTemp("", "zyvor-measure-install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	e, sim, _, asg, cleanup, err := newDevice(dir, "measure-install", "1.0.1", 1, nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	start := time.Now()
	if _, err = e.Submit(asg); err != nil {
		return nil, err
	}
	steps, err := until(e, ota.Committed, 30*time.Second)
	if err != nil {
		return nil, err
	}
	wall := time.Since(start)
	st, err := sim.Status(context.Background())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"artifact_bytes":        artifactBytes,
		"wall_ms":               wall.Milliseconds(),
		"steps":                 steps,
		"final_state":           string(ota.Committed),
		"booted_slot":           st.Booted,
		"state_dir_bytes":       dirBytes(dir),
		"journal_bytes":         fileBytes(filepath.Join(dir, "ota.db")),
		"journal_wal_bytes":     fileBytes(filepath.Join(dir, "ota.db-wal")),
		"cache_bytes":           dirBytes(filepath.Join(dir, "cache")),
		"health_stable_seconds": e.Config.HealthStableSeconds,
	}, nil
}

func measureRollback() (map[string]any, error) {
	dir, err := os.MkdirTemp("", "zyvor-measure-rollback-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	e, sim, _, asg, cleanup, err := newDevice(dir, "measure-rollback", "1.0.2", 2, func(context.Context) error {
		return errors.New("device unhealthy")
	})
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, err = e.Submit(asg); err != nil {
		return nil, err
	}
	start := time.Now()
	steps, err := until(e, ota.RolledBack, 30*time.Second)
	if err != nil {
		return nil, err
	}
	st, err := sim.Status(context.Background())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"wall_ms":                time.Since(start).Milliseconds(),
		"steps":                  steps,
		"final_state":            string(ota.RolledBack),
		"booted_slot":            st.Booted,
		"health_timeout_seconds": e.Config.HealthTimeoutSeconds,
		"note":                   "Wall time includes the configured health deadline. The simulator restores the previous slot in-process.",
	}, nil
}

func measureRecoverAbort() (map[string]any, error) {
	dir, err := os.MkdirTemp("", "zyvor-measure-recover-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	e, sim, _, asg, cleanup, err := newDevice(dir, "measure-recover", "1.0.3", 3, nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	sim.FailInstall = true
	if _, err = e.Submit(asg); err != nil {
		return nil, err
	}
	toRecovery := time.Now()
	steps, err := until(e, ota.NeedsRecovery, 30*time.Second)
	if err != nil {
		return nil, err
	}
	recoveryWall := time.Since(toRecovery)
	sim.FailInstall = false
	abortStart := time.Now()
	if err = e.RecoverAbort(context.Background()); err != nil {
		return nil, err
	}
	abortWall := time.Since(abortStart)
	state := e.Store.View().Jobs[asg.JobID].State
	return map[string]any{
		"time_to_needs_recovery_ms": recoveryWall.Milliseconds(),
		"steps_to_needs_recovery":   steps,
		"recover_abort_wall_ms":     abortWall.Milliseconds(),
		"final_state":               string(state),
		"note":                      "NeedsRecovery is the ambiguous install path. recover-abort is the operator call on the simulator, not a board power cycle.",
	}, nil
}

func measureRelay() (map[string]any, error) {
	body := bytes.Repeat([]byte("z"), artifactBytes)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var upstreamBytes atomic.Int64
	var fetches atomic.Int64
	s := &relay.Server{
		Token: "measure-token",
		Upstream: func(sha string) (io.ReadCloser, error) {
			if sha != digest {
				return nil, errors.New("digest")
			}
			fetches.Add(1)
			return io.NopCloser(&countReader{r: bytes.NewReader(body), n: &upstreamBytes}), nil
		},
	}
	dir, err := os.MkdirTemp("", "zyvor-measure-relay-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	s.Dir = dir
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	const clients = 100
	var clientBytes atomic.Int64
	var wg sync.WaitGroup
	errCh := make(chan error, clients)
	start := time.Now()
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/artifacts/"+digest, nil)
			if err != nil {
				errCh <- err
				return
			}
			req.Header.Set("Authorization", "Bearer measure-token")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("status %d", resp.StatusCode)
				return
			}
			n, err := io.Copy(io.Discard, resp.Body)
			if err != nil {
				errCh <- err
				return
			}
			clientBytes.Add(n)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return nil, err
	}
	up := upstreamBytes.Load()
	clientsTotal := clientBytes.Load()
	return map[string]any{
		"clients":            clients,
		"artifact_bytes":     artifactBytes,
		"upstream_fetches":   fetches.Load(),
		"upstream_bytes":     up,
		"client_bytes":       clientsTotal,
		"wan_bytes_not_sent": clientsTotal - up,
		"wall_ms":            time.Since(start).Milliseconds(),
		"note":               "Localhost relay. WAN bytes not sent are client bytes minus the single upstream read. This is not a wide-area link.",
	}, nil
}

func newDevice(dir, id, version string, seq uint64, health ota.HealthFunc) (*ota.Engine, *ota.Simulator, ota.Release, ota.Assignment, func(), error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, ota.Release{}, ota.Assignment{}, nil, err
	}
	payload := bytes.Repeat([]byte("b"), artifactBytes)
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	u, err := url.Parse(server.URL)
	if err != nil {
		server.Close()
		return nil, nil, ota.Release{}, ota.Assignment{}, nil, err
	}
	c := ota.Config{
		DeviceID: id, Compatible: "measure-board", StateDir: dir,
		Socket: filepath.Join(dir, "agent.sock"), Backend: "simulator",
		TrustKeys:            map[string]string{"measure": base64.StdEncoding.EncodeToString(pub)},
		DownloadHosts:        []string{u.Host},
		MaxArtifactBytes:     8 << 20,
		HealthTimeoutSeconds: 1,
		HealthStableSeconds:  0,
	}
	if err = c.Validate(); err != nil {
		server.Close()
		return nil, nil, ota.Release{}, ota.Assignment{}, nil, err
	}
	store, err := ota.OpenStore(dir)
	if err != nil {
		server.Close()
		return nil, nil, ota.Release{}, ota.Assignment{}, nil, err
	}
	sim, err := ota.NewSimulator(dir, c.Compatible)
	if err != nil {
		_ = store.Close()
		server.Close()
		return nil, nil, ota.Release{}, ota.Assignment{}, nil, err
	}
	e := ota.NewEngine(c, store, sim)
	if health != nil {
		e.Health = health
	}
	rel := ota.Release{
		Schema: 1, ID: id, Sequence: seq, Compatible: c.Compatible, Backend: c.Backend,
		Version: version, Expires: time.Now().Add(time.Hour),
		Artifact: ota.Artifact{URL: server.URL + "/os.raucb", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload))},
	}
	env, err := ota.SignRelease(rel, "measure", priv)
	if err != nil {
		_ = store.Close()
		server.Close()
		return nil, nil, ota.Release{}, ota.Assignment{}, nil, err
	}
	asg := ota.Assignment{
		JobID: "job-" + id, DeviceID: c.DeviceID, Release: env,
		NotBefore: time.Now().Add(-time.Minute), Deadline: time.Now().Add(30 * time.Minute),
		AutoReboot: true,
	}
	cleanup := func() {
		_ = store.Close()
		server.Close()
	}
	return e, sim, rel, asg, cleanup, nil
}

func until(e *ota.Engine, want ota.State, limit time.Duration) (int, error) {
	deadline := time.Now().Add(limit)
	steps := 0
	for time.Now().Before(deadline) {
		view := e.Store.View()
		for _, j := range view.Jobs {
			if j.State == want {
				return steps, nil
			}
		}
		steps++
		if err := e.Step(context.Background()); err != nil {
			return steps, err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return steps, fmt.Errorf("timed out waiting for %s", want)
}

func dirBytes(root string) int64 {
	var n int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() {
			return nil
		}
		n += info.Size()
		return nil
	})
	return n
}

func fileBytes(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func rssKB() int {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

type rssPeak struct {
	mu   sync.Mutex
	peak int
}

func (p *rssPeak) watch(ctx context.Context) {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n := rssKB()
			p.mu.Lock()
			if n > p.peak {
				p.peak = n
			}
			p.mu.Unlock()
		}
	}
}

func (p *rssPeak) reset() {
	p.mu.Lock()
	p.peak = rssKB()
	p.mu.Unlock()
}

func (p *rssPeak) max() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n := rssKB(); n > p.peak {
		p.peak = n
	}
	return p.peak
}

type countReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}
