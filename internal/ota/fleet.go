// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Fleet is an outbound-only versioned integration contract, not an assertion
// that an existing Zyvor Fleet deployment already implements these endpoints.
type Fleet struct {
	Config Config
	Client *http.Client
	token  string
}

func NewFleet(c Config) (*Fleet, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.FleetCA != "" {
		b, e := os.ReadFile(c.FleetCA)
		if e != nil {
			return nil, e
		}
		roots, e := x509.SystemCertPool()
		if e != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(b) {
			return nil, errors.New("invalid Fleet CA")
		}
		tlsConfig.RootCAs = roots
	}
	if c.FleetCert != "" || c.FleetKey != "" {
		cert, e := tls.LoadX509KeyPair(c.FleetCert, c.FleetKey)
		if e != nil {
			return nil, e
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	f := &Fleet{Config: c, Client: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Fleet redirects forbidden") }}}
	if c.FleetTokenFile != "" {
		b, e := os.ReadFile(c.FleetTokenFile)
		if e != nil {
			return nil, e
		}
		f.token = strings.TrimSpace(string(b))
		if f.token == "" {
			return nil, errors.New("empty Fleet token")
		}
	}
	return f, nil
}
func (f *Fleet) request(ctx context.Context, method, suffix string, body any) ([]byte, int, error) {
	var b []byte
	var err error
	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(f.Config.FleetURL, "/")+"/v1/devices/"+f.Config.DeviceID+suffix, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, 0, errors.New("Fleet request failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (512<<10)+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(data) > 512<<10 {
		return nil, resp.StatusCode, errors.New("Fleet response too large")
	}
	return data, resp.StatusCode, nil
}
func (f *Fleet) Sync(ctx context.Context, e *Engine) error {
	started := time.Now()
	err := f.sync(ctx, e)
	e.Stats.fleetSync(err == nil, time.Since(started))
	if e.Spans != nil {
		e.Spans.Export(fleetSpan(e.Config.DeviceID, started, time.Now(), err))
	}
	return err
}
func (f *Fleet) sync(ctx context.Context, e *Engine) error {
	events := e.Store.View().Events
	if len(events) > 100 {
		events = events[:100]
	}
	if len(events) > 0 {
		b, status, err := f.request(ctx, http.MethodPost, "/events", events)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("Fleet event HTTP %d", status)
		}
		var ack struct {
			Sequence uint64 `json:"sequence"`
		}
		if err = StrictJSON(b, &ack); err != nil {
			return err
		}
		if ack.Sequence > events[len(events)-1].Sequence {
			return errors.New("Fleet acknowledged unsent events")
		}
		if err = e.Ack(ack.Sequence); err != nil {
			return err
		}
	}
	b, status, err := f.request(ctx, http.MethodGet, "/assignment", nil)
	if err != nil {
		return err
	}
	if status == 204 {
		return nil
	}
	if status != 200 {
		return fmt.Errorf("Fleet assignment HTTP %d", status)
	}
	var a Assignment
	if err = StrictJSON(b, &a); err != nil {
		return err
	}
	_, err = e.Submit(a)
	return err
}
