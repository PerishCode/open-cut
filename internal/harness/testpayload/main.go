package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func main() {
	var descriptor protocol.ControlDescriptor
	if err := json.Unmarshal([]byte(required("OC_SIDECAR_CONTROL")), &descriptor); err != nil {
		fatal(err)
	}
	session, err := client.DialSession(context.Background(), descriptor, required("OC_SIDECAR_TOKEN"), client.Registration{
		Channel: required("OC_SIDECAR_CHANNEL"), Namespace: required("OC_SIDECAR_NAMESPACE"),
		App: "fixture-runtime", Mode: required("OC_SIDECAR_MODE"), Source: required("OC_SIDECAR_SOURCE"),
	})
	if err != nil {
		fatal(err)
	}
	control := client.New(descriptor, required("OC_SIDECAR_TOKEN"))
	defer session.Close(0)
	delay := durationFromEnv("OC_FIXTURE_READY_DELAY_MS", 100*time.Millisecond)
	lifetime := durationFromEnv("OC_FIXTURE_LIFETIME_MS", 1200*time.Millisecond)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	readyTimer := time.NewTimer(delay)
	defer readyTimer.Stop()
	lifetimeTimer := time.NewTimer(lifetime)
	defer lifetimeTimer.Stop()
	ready := false
	var updateTimer <-chan time.Time
	for {
		select {
		case <-ticker.C:
			if err := session.Heartbeat(); err != nil {
				fatal(err)
			}
		case <-readyTimer.C:
			if !ready {
				if err := session.Ready(); err != nil {
					fatal(err)
				}
				ready = true
				if os.Getenv("OC_FIXTURE_REQUEST_UPDATE_FROM") == os.Getenv("OC_RELEASE_VERSION") {
					timer := time.NewTimer(durationFromEnv("OC_FIXTURE_UPDATE_DELAY_MS", 750*time.Millisecond))
					defer timer.Stop()
					updateTimer = timer.C
				}
			}
		case <-updateTimer:
			transition, err := control.PrepareLatest(context.Background())
			if err != nil {
				fatal(err)
			}
			if transition.RestartRequired {
				if _, err := control.Control(context.Background(), "shutdown"); err != nil {
					fatal(err)
				}
				return
			}
			updateTimer = nil
		case <-lifetimeTimer.C:
			if _, err := control.Control(context.Background(), "shutdown"); err != nil {
				fatal(err)
			}
			return
		}
	}
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		fatal(fmt.Errorf("%s is required", name))
	}
	return value
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	milliseconds, err := strconv.Atoi(value)
	if err != nil {
		fatal(err)
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
