// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zyvorai/ota/internal/ota"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	socket := flag.String("socket", "/run/zyvor-ota/agent.sock", "agent Unix socket")
	simulationURL := flag.String("simulation-url", "", "simulator-only loopback HTTP URL")
	flag.Parse()
	a := flag.Args()
	if len(a) == 0 {
		return errors.New("usage: zyvor-ota [-socket PATH] version|keygen DIR|sign RELEASE KEY KEY_ID OUT|status|job ID|submit ASSIGNMENT|events|ack SEQUENCE|reboot|recover-abort|gc")
	}
	switch a[0] {
	case "version":
		fmt.Println(ota.Version)
		return nil
	case "keygen":
		if len(a) != 2 {
			return errors.New("keygen DIR")
		}
		if err := os.Mkdir(a[1], 0700); err != nil {
			return err
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err = ota.AtomicWrite(filepath.Join(a[1], "release.key"), []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0600); err != nil {
			return err
		}
		return ota.AtomicWrite(filepath.Join(a[1], "release.pub"), []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0644)
	case "sign":
		if len(a) != 5 {
			return errors.New("sign RELEASE_JSON PRIVATE_KEY KEY_ID OUTPUT_JSON")
		}
		b, err := os.ReadFile(a[1])
		if err != nil {
			return err
		}
		var release ota.Release
		if err = ota.StrictJSON(b, &release); err != nil {
			return err
		}
		b, err = os.ReadFile(a[2])
		if err != nil {
			return err
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil {
			return err
		}
		env, err := ota.SignRelease(release, a[3], key)
		if err != nil {
			return err
		}
		b, err = json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}
		return ota.AtomicWrite(a[4], b, 0644)
	}
	method := http.MethodGet
	path := ""
	var body []byte
	switch a[0] {
	case "status":
		path = "/v1/status"
	case "events":
		path = "/v1/events"
	case "job":
		if len(a) != 2 || strings.ContainsAny(a[1], "/?#") {
			return errors.New("job ID")
		}
		path = "/v1/jobs/" + a[1]
	case "submit":
		if len(a) != 2 {
			return errors.New("submit ASSIGNMENT_JSON")
		}
		var err error
		body, err = os.ReadFile(a[1])
		if err != nil {
			return err
		}
		method = http.MethodPost
		path = "/v1/assignments"
	case "ack":
		if len(a) != 2 {
			return errors.New("ack SEQUENCE")
		}
		body = []byte(`{"sequence":` + a[1] + `}`)
		method = http.MethodPost
		path = "/v1/events/ack"
	case "reboot", "recover-abort", "gc":
		method = http.MethodPost
		path = "/v1/" + a[0]
	default:
		return errors.New("unknown command")
	}
	base := "http://unix"
	client := ota.UnixClient(*socket)
	if *simulationURL != "" {
		u, e := url.Parse(*simulationURL)
		if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("simulation-url must be http://127.0.0.1:PORT")
		}
		base = *simulationURL
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("agent HTTP %d", resp.StatusCode)
	}
	return nil
}
