// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

type HealthFunc func(context.Context) error

func LocalHealth(checks []Check) HealthFunc {
	return func(ctx context.Context) error {
		for _, check := range checks {
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			var err error
			switch check.Kind {
			case "file":
				_, err = os.Stat(check.Target)
			case "systemd":
				err = exec.CommandContext(c, "/usr/bin/systemctl", "is-active", "--quiet", check.Target).Run()
			case "http":
				var req *http.Request
				req, err = http.NewRequestWithContext(c, http.MethodGet, check.Target, nil)
				if err == nil {
					client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
					var resp *http.Response
					resp, err = client.Do(req)
					if err == nil {
						resp.Body.Close()
						if resp.StatusCode != 200 {
							err = fmt.Errorf("HTTP %d", resp.StatusCode)
						}
					}
				}
			default:
				err = fmt.Errorf("unknown health check")
			}
			cancel()
			if err != nil {
				name := check.Name
				if name == "" {
					name = check.Kind + ":" + check.Target
				}
				return &HealthError{Name: name, Err: fmt.Errorf("%s health check failed", check.Kind)}
			}
		}
		return nil
	}
}

// HealthError names the check that failed so metrics can count it.
type HealthError struct {
	Name string
	Err  error
}

func (e *HealthError) Error() string { return e.Err.Error() }
func (e *HealthError) Unwrap() error { return e.Err }
