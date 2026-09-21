// SPDX-License-Identifier: Apache-2.0
// Package otaclient is the supported Go client for the local operator API
// described in api/openapi.yaml.
//
// operationId: status
// operationId: getJob
// operationId: submitAssignment
// operationId: getEvents
// operationId: acknowledgeEvents
// operationId: requestReboot
// operationId: recoverAbort
// operationId: cleanCache
// operationId: metrics
package otaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/zyvorai/ota/internal/ota"
)

type Client struct {
	HTTP *http.Client
}

func Unix(path string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}}
}

type Status struct {
	Version       string   `json:"version"`
	DeviceID      string   `json:"device_id"`
	Backend       string   `json:"backend"`
	Active        *ota.Job `json:"active"`
	HighSequence  uint64   `json:"high_sequence"`
	EventSequence uint64   `json:"event_sequence"`
	PendingEvents int      `json:"pending_events"`
}

type ErrorBody struct {
	Error string `json:"error"`
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	var body ErrorBody
	if json.Unmarshal([]byte(e.Body), &body) == nil && body.Error != "" {
		return fmt.Sprintf("ota api %d: %s", e.Status, body.Error)
	}
	return fmt.Sprintf("ota api %d: %s", e.Status, e.Body)
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var out Status
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, 200, &out)
	return out, err
}

func (c *Client) Job(ctx context.Context, id string) (ota.Job, error) {
	var out ota.Job
	err := c.do(ctx, http.MethodGet, "/v1/jobs/"+id, nil, 200, &out)
	return out, err
}

func (c *Client) Submit(ctx context.Context, a ota.Assignment) (ota.Job, error) {
	var out ota.Job
	err := c.do(ctx, http.MethodPost, "/v1/assignments", a, 202, &out)
	return out, err
}

func (c *Client) Events(ctx context.Context) ([]ota.Event, error) {
	var out []ota.Event
	err := c.do(ctx, http.MethodGet, "/v1/events", nil, 200, &out)
	return out, err
}

func (c *Client) Ack(ctx context.Context, sequence uint64) error {
	return c.do(ctx, http.MethodPost, "/v1/events/ack", map[string]uint64{"sequence": sequence}, 200, nil)
}

func (c *Client) Reboot(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/reboot", nil, 200, nil)
}

func (c *Client) RecoverAbort(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/recover-abort", nil, 200, nil)
}

func (c *Client) GC(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/gc", nil, 200, nil)
}

func (c *Client) Metrics(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/metrics", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	return string(b), nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, want int, dest any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != want {
		return &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(b, dest)
}
