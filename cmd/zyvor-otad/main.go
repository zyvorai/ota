// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/zyvorai/ota/internal/ota"
)

func main() {
	if err := run(); err != nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	config := flag.String("config", "/etc/zyvor-ota/agent.json", "configuration path")
	simListen := flag.String("simulation-listen", "", "simulator-only loopback HTTP address (development)")
	flag.Parse()
	b, err := os.ReadFile(*config)
	if err != nil {
		return err
	}
	var c ota.Config
	if err = ota.StrictJSON(b, &c); err != nil {
		return err
	}
	if err = c.Validate(); err != nil {
		return err
	}
	store, err := ota.OpenStore(c.StateDir)
	if err != nil {
		return err
	}
	defer store.Close()
	var backend ota.Backend
	if c.Backend == "simulator" {
		backend, err = ota.NewSimulator(c.StateDir, c.Compatible)
	} else {
		var r *ota.RAUC
		r, err = ota.NewRAUC()
		if err == nil {
			defer r.Conn.Close()
			backend = r
		}
	}
	if err != nil {
		return err
	}
	engine := ota.NewEngine(c, store, backend)
	var traces *ota.OTLPExporter
	if c.OTLPEndpoint != "" {
		traces, err = ota.NewOTLPExporter(c)
		if err != nil {
			return err
		}
		engine.Spans = traces
	}
	var fleet *ota.Fleet
	if c.FleetURL != "" {
		fleet, err = ota.NewFleet(c)
		if err != nil {
			return err
		}
	}
	var l net.Listener
	if *simListen != "" {
		host, _, parseErr := net.SplitHostPort(*simListen)
		if c.Backend != "simulator" || parseErr != nil || host != "127.0.0.1" {
			return fmt.Errorf("simulation-listen requires simulator backend and 127.0.0.1")
		}
		l, err = net.Listen("tcp", *simListen)
	} else {
		l, err = ota.ListenUnix(c.Socket)
	}
	if err != nil {
		return err
	}
	defer l.Close()
	if *simListen == "" {
		defer os.Remove(c.Socket)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Handler: engine.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(l) }()
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := engine.Step(ctx); err != nil {
					job, release, campaign := activeIDs(engine)
					slog.Warn("update step deferred", "error", err, "device_id", c.DeviceID, "job_id", job, "release_id", release, "campaign_id", campaign)
				}
			}
		}
	}()
	if fleet != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			tick := time.NewTicker(10 * time.Second)
			defer tick.Stop()
			for {
				if err := fleet.Sync(ctx, engine); err != nil && ctx.Err() == nil {
					job, release, campaign := activeIDs(engine)
					slog.Warn("Fleet sync deferred", "error", err, "device_id", c.DeviceID, "job_id", job, "release_id", release, "campaign_id", campaign)
				}
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}()
	}
	slog.Info("Zyvor OTA ready", "version", ota.Version, "backend", c.Backend, "device_id", c.DeviceID, "listener", l.Addr().String())
	select {
	case <-ctx.Done():
	case err = <-serverErrors:
		cancel()
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdown)
	cancel()
	workers.Wait()
	if traces != nil {
		shut, cancelShut := context.WithTimeout(context.Background(), 2*time.Second)
		traces.Shutdown(shut)
		cancelShut()
	}
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server: %w", err)
	}
	return nil
}

func activeIDs(engine *ota.Engine) (job, release, campaign string) {
	d := engine.Store.View()
	j, ok := d.Jobs[d.Active]
	if !ok {
		return "", "", ""
	}
	return j.Assignment.JobID, j.Release.ID, j.Assignment.CampaignID
}
