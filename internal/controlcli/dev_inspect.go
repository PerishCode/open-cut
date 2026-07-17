package controlcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/sidecar/client"
)

// runDevInspect reaches the live development cell's Electron renderer through
// the sidecar-published loopback CDP endpoint: control descriptor and owner
// token select the cell, the session endpoint selects the renderer.
func runDevInspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("dev inspect", flag.ContinueOnError)
	set.SetOutput(stderr)
	repository := set.String("repo", ".", "open-cut repository root")
	baseDir := set.String("base-dir", "", "development base directory; defaults below the repository")
	screenshot := set.String("screenshot", "", "write a PNG capture of the live renderer to this path")
	evaluate := set.String("eval", "", "JavaScript expression to evaluate in the live renderer")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if *screenshot == "" && *evaluate == "" {
		fmt.Fprintln(stderr, "dev inspect requires --screenshot and/or --eval")
		return 2
	}
	repositoryRoot, err := filepath.Abs(*repository)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	selectedBaseDir, err := devsession.ResolveBaseDir(repositoryRoot, *baseDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	paths, err := devsession.ResolveCellPaths(selectedBaseDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if err != nil {
		fmt.Fprintf(stderr, "dev inspect requires a running development cell: %v\n", err)
		return 1
	}
	requestContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, err := owner.Status(requestContext)
	if err != nil {
		fmt.Fprintf(stderr, "read development cell status: %v\n", err)
		return 1
	}
	endpoint := ""
	for _, session := range status.Sessions {
		for _, candidate := range session.Endpoints {
			if candidate.Name == devsession.PayloadCDPEndpointName {
				endpoint = candidate.URL
			}
		}
	}
	if endpoint == "" {
		fmt.Fprintln(stderr, "the development cell does not expose a payload CDP endpoint; restart oc-control dev")
		return 1
	}
	cdp, err := businessacceptance.ConnectCreatorCDP(requestContext, endpoint)
	if err != nil {
		fmt.Fprintf(stderr, "connect development renderer: %v\n", err)
		return 1
	}
	defer cdp.Close()
	result := map[string]any{"schema": 1, "endpoint": endpoint}
	if *screenshot != "" {
		var capture struct {
			Data string `json:"data"`
		}
		if err := cdp.Call(requestContext, "Page.captureScreenshot", map[string]any{"format": "png"}, &capture); err != nil {
			fmt.Fprintf(stderr, "capture development renderer: %v\n", err)
			return 1
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(capture.Data)
		if decodeErr != nil {
			fmt.Fprintf(stderr, "decode development capture: %v\n", decodeErr)
			return 1
		}
		destination, absErr := filepath.Abs(*screenshot)
		if absErr != nil {
			fmt.Fprintln(stderr, absErr)
			return 1
		}
		if err := os.WriteFile(destination, decoded, 0o644); err != nil {
			fmt.Fprintf(stderr, "write development capture: %v\n", err)
			return 1
		}
		result["screenshot"] = destination
		result["screenshotBytes"] = len(decoded)
	}
	if *evaluate != "" {
		var evaluated struct {
			Result struct {
				Type  string `json:"type"`
				Value any    `json:"value"`
			} `json:"result"`
			Exception json.RawMessage `json:"exceptionDetails"`
		}
		if err := cdp.Call(requestContext, "Runtime.evaluate", map[string]any{
			"expression": *evaluate, "returnByValue": true, "awaitPromise": true,
		}, &evaluated); err != nil {
			fmt.Fprintf(stderr, "evaluate in development renderer: %v\n", err)
			return 1
		}
		if len(evaluated.Exception) > 0 && string(evaluated.Exception) != "null" {
			fmt.Fprintln(stderr, "development renderer expression raised an exception")
			return 1
		}
		result["valueType"] = evaluated.Result.Type
		result["value"] = evaluated.Result.Value
	}
	return writeOutput(stdout, stderr, result)
}
