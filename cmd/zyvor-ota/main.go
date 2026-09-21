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
		return errors.New("usage: otactl [-socket PATH] version|keygen DIR|sign RELEASE KEY KEY_ID OUT|campaign-export ASSIGNMENT ARTIFACT_DIR OUT_DIR|campaign-import CAMPAIGN_DIR MEDIA_DIR|status [json]|job ID|submit ASSIGNMENT|events|ack SEQUENCE|reboot|recover-abort|gc|backup DEST|archive  (zyvor-ota is the same binary)")
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
	case "campaign-export":
		if len(a) != 4 {
			return errors.New("campaign-export ASSIGNMENT_JSON ARTIFACT_DIR OUT_DIR")
		}
		b, err := os.ReadFile(a[1])
		if err != nil {
			return err
		}
		var assignment ota.Assignment
		if err = ota.StrictJSON(b, &assignment); err != nil {
			return err
		}
		return ota.ExportCampaign(a[3], assignment, a[2])
	case "campaign-import":
		if len(a) != 3 {
			return errors.New("campaign-import CAMPAIGN_DIR MEDIA_DIR")
		}
		assignment, err := ota.ImportCampaign(a[1])
		if err != nil {
			return err
		}
		if err = ota.InstallCampaign(a[2], a[1], assignment); err != nil {
			return err
		}
		fmt.Println(filepath.Join(a[1], "assignment.json"))
		return nil
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
	case "backup":
		if len(a) != 2 || !filepath.IsAbs(a[1]) {
			return errors.New("backup DEST must be an absolute path")
		}
		var err error
		body, err = json.Marshal(map[string]string{"path": a[1]})
		if err != nil {
			return err
		}
		method = http.MethodPost
		path = "/v1/backup"
	case "archive":
		path = "/v1/archive"
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
		if a[0] == "status" && (len(a) < 2 || a[1] != "json") {
			fmt.Print(formatOTAStatus(nil, err.Error()))
		}
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if a[0] == "status" && (len(a) < 2 || a[1] != "json") {
		msg := ""
		if resp.StatusCode >= 300 {
			msg = fmt.Sprintf("agent HTTP %d", resp.StatusCode)
		}
		fmt.Print(formatOTAStatus(b, msg))
		if resp.StatusCode >= 300 {
			return fmt.Errorf("agent HTTP %d", resp.StatusCode)
		}
		return nil
	}
	fmt.Println(string(b))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("agent HTTP %d", resp.StatusCode)
	}
	return nil
}

func formatOTAStatus(b []byte, collection string) string {
	view := StatusView{Labels: [5]string{"Agent", "Backend", "Active job", "Events", "Sequence"}}
	if collection != "" && len(b) == 0 {
		for i := range view.Components {
			view.Components[i].Disabled = true
		}
		view.Collection = []string{collection}
		return view.Format()
	}
	var st struct {
		Version       string          `json:"version"`
		DeviceID      string          `json:"device_id"`
		Backend       string          `json:"backend"`
		Active        json.RawMessage `json:"active"`
		HighSequence  uint64          `json:"high_sequence"`
		PendingEvents int             `json:"pending_events"`
	}
	_ = json.Unmarshal(b, &st)
	if st.Backend == "" {
		view.Components[1].Disabled = true
	}
	if len(st.Active) == 0 || string(st.Active) == "null" {
		view.Components[2].Disabled = true
	}
	if st.PendingEvents > 0 {
		view.Components[3].Warnings = st.PendingEvents
	}
	view.Body = [][3]string{
		{"🖥️  Device:", st.DeviceID, ""},
		{"📦 Backend:", st.Backend, ""},
		{"🚀 Version:", st.Version, ""},
		{"🔌 Pending events:", fmt.Sprintf("%d", st.PendingEvents), ""},
		{"🖼️  Sequence:", fmt.Sprintf("%d", st.HighSequence), ""},
	}
	for _, name := range []string{"Sign", "Submit", "Ack", "Reboot", "Recover", "GC", "Events", "RAUC"} {
		on := true
		if name == "RAUC" && st.Backend != "" && st.Backend != "rauc" {
			on = false
		}
		view.Features = append(view.Features, okFeature(name, on))
	}
	if collection != "" {
		view.Collection = []string{collection}
	}
	return view.Format()
}
