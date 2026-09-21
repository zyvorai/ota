// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Span is one OpenTelemetry span for a job transition or a Fleet sync.
type Span struct {
	TraceID  string
	SpanID   string
	ParentID string
	Name     string
	Start    time.Time
	End      time.Time
	Attrs    map[string]string
	Error    bool
}

// SpanExporter accepts spans. Export must not block the state machine for long.
type SpanExporter interface {
	Export(Span)
}

func (c Config) validateOTLP() error {
	if c.OTLPEndpoint == "" {
		if c.OTLPTokenFile != "" {
			return errors.New("otlp_token_file requires otlp_endpoint")
		}
		return nil
	}
	u, err := url.Parse(c.OTLPEndpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return errors.New("otlp_endpoint must be a plain base URL")
	}
	local := u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if u.Scheme != "https" && !local {
		return errors.New("otlp_endpoint must be HTTPS or loopback HTTP")
	}
	if u.Path != "" && u.Path != "/" && u.Path != "/v1/traces" {
		return errors.New("otlp_endpoint path must be empty or /v1/traces")
	}
	return nil
}

func (e *Engine) nextSpan(j Job, prev time.Time, message string) (Span, Job, error) {
	if j.TraceID == "" {
		id, err := randomHex(16)
		if err != nil {
			return Span{}, j, err
		}
		j.TraceID = id
	}
	parent := j.SpanID
	id, err := randomHex(8)
	if err != nil {
		return Span{}, j, err
	}
	j.SpanID = id
	start := prev
	if start.IsZero() || start.After(j.Updated) {
		start = j.Updated
	}
	attrs := map[string]string{
		"job_id":      j.Assignment.JobID,
		"release_id":  j.Release.ID,
		"device_id":   e.Config.DeviceID,
		"campaign_id": j.Assignment.CampaignID,
		"state":       string(j.State),
	}
	if message != "" {
		attrs["error"] = message
	}
	return Span{
		TraceID: j.TraceID, SpanID: id, ParentID: parent,
		Name: "ota." + string(j.State), Start: start, End: j.Updated,
		Attrs: attrs, Error: j.State == Failed || j.State == NeedsRecovery,
	}, j, nil
}

func fleetSpan(deviceID string, start, end time.Time, err error) Span {
	trace, _ := randomHex(16)
	span, _ := randomHex(8)
	attrs := map[string]string{"device_id": deviceID, "result": "ok"}
	if err != nil {
		attrs["result"] = "error"
		attrs["error"] = err.Error()
	}
	return Span{
		TraceID: trace, SpanID: span, Name: "ota.fleet_sync",
		Start: start, End: end, Attrs: attrs, Error: err != nil,
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// OTLPExporter posts OTLP/HTTP JSON traces. It drops spans when the collector is slow.
type OTLPExporter struct {
	endpoint string
	token    string
	client   *http.Client
	spans    chan Span
	done     chan struct{}
}

func NewOTLPExporter(c Config) (*OTLPExporter, error) {
	if err := c.validateOTLP(); err != nil {
		return nil, err
	}
	if c.OTLPEndpoint == "" {
		return nil, errors.New("otlp_endpoint is empty")
	}
	token := ""
	if c.OTLPTokenFile != "" {
		b, err := os.ReadFile(c.OTLPTokenFile)
		if err != nil {
			return nil, err
		}
		token = strings.TrimSpace(string(b))
		if token == "" {
			return nil, errors.New("empty otlp token")
		}
	}
	e := &OTLPExporter{
		endpoint: c.OTLPEndpoint,
		token:    token,
		client: &http.Client{
			Timeout: 2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("otlp redirects forbidden")
			},
		},
		spans: make(chan Span, 32),
		done:  make(chan struct{}),
	}
	go e.loop()
	return e, nil
}

func (e *OTLPExporter) Export(s Span) {
	select {
	case e.spans <- s:
	default:
		slog.Warn("otlp span dropped")
	}
}

func (e *OTLPExporter) Shutdown(ctx context.Context) {
	close(e.spans)
	select {
	case <-e.done:
	case <-ctx.Done():
	}
}

func (e *OTLPExporter) loop() {
	defer close(e.done)
	for s := range e.spans {
		if err := e.post(s); err != nil {
			slog.Warn("otlp export failed")
		}
	}
}

func (e *OTLPExporter) post(s Span) error {
	body, err := json.Marshal(otlpPayload(s))
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, tracesURL(e.endpoint), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("otlp status")
	}
	return nil
}

func tracesURL(endpoint string) string {
	base := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(base, "/v1/traces") {
		return base
	}
	return base + "/v1/traces"
}

func otlpPayload(s Span) map[string]any {
	status := 1
	if s.Error {
		status = 2
	}
	span := map[string]any{
		"traceId":           s.TraceID,
		"spanId":            s.SpanID,
		"name":              s.Name,
		"kind":              1,
		"startTimeUnixNano": unixNano(s.Start),
		"endTimeUnixNano":   unixNano(s.End),
		"attributes":        otlpAttrs(s.Attrs),
		"status":            map[string]any{"code": status},
	}
	if s.ParentID != "" {
		span["parentSpanId"] = s.ParentID
	}
	return map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": otlpAttrs(map[string]string{"service.name": "zyvor-otad"}),
				},
				"scopeSpans": []any{
					map[string]any{
						"scope": map[string]any{"name": "github.com/zyvorai/ota"},
						"spans": []any{span},
					},
				},
			},
		},
	}
}

func otlpAttrs(attrs map[string]string) []any {
	out := make([]any, 0, len(attrs))
	for k, v := range attrs {
		if v == "" {
			continue
		}
		out = append(out, map[string]any{
			"key": k, "value": map[string]any{"stringValue": v},
		})
	}
	return out
}

func unixNano(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatInt(t.UnixNano(), 10)
}
