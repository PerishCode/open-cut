package controlcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/sidecar/client"
)

// connectDevCell loads the live development cell's owner client so tooling
// can read status and broadcast control commands.
func connectDevCell(repository, baseDir string) (*client.Client, error) {
	repositoryRoot, err := filepath.Abs(repository)
	if err != nil {
		return nil, err
	}
	selectedBaseDir, err := devsession.ResolveBaseDir(repositoryRoot, baseDir)
	if err != nil {
		return nil, err
	}
	paths, err := devsession.ResolveCellPaths(selectedBaseDir)
	if err != nil {
		return nil, err
	}
	owner, err := client.Load(paths.ControlFile, paths.OwnerTokenFile)
	if err != nil {
		return nil, fmt.Errorf("a running development cell is required: %w", err)
	}
	return owner, nil
}

// connectDevRenderer reaches a controlled renderer over loopback CDP. With an
// explicit endpoint it attaches directly — any potentially controlled program
// qualifies; otherwise the development cell's control descriptor and owner
// token select the cell and its published payload endpoint.
func connectDevRenderer(
	ctx context.Context,
	repository, baseDir, explicitEndpoint string,
	stderr io.Writer,
) (*businessacceptance.CDPClient, string, error) {
	if explicitEndpoint != "" {
		cdp, err := businessacceptance.ConnectCreatorCDP(ctx, explicitEndpoint)
		if err != nil {
			return nil, "", fmt.Errorf("connect controlled renderer: %w", err)
		}
		return cdp, explicitEndpoint, nil
	}
	owner, err := connectDevCell(repository, baseDir)
	if err != nil {
		return nil, "", err
	}
	statusContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	status, err := owner.Status(statusContext)
	if err != nil {
		return nil, "", fmt.Errorf("read development cell status: %w", err)
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
		return nil, "", fmt.Errorf("the development cell does not expose a payload CDP endpoint; restart oc-control dev")
	}
	cdp, err := businessacceptance.ConnectCreatorCDP(ctx, endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("connect development renderer: %w", err)
	}
	_ = stderr
	return cdp, endpoint, nil
}

type devInspectOptions struct {
	repository, baseDir, screenshot, evaluate, setFile, endpoint string
}

func newDevInspectCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "inspect", Short: "Inspect the live development renderer over CDP", Args: cobra.NoArgs}
	options := devInspectOptions{}
	command.Flags().StringVar(&options.repository, "repo", ".", "open-cut repository root")
	command.Flags().StringVar(&options.baseDir, "base-dir", "", "development base directory; defaults below the repository")
	command.Flags().StringVar(&options.screenshot, "screenshot", "", "write a PNG capture of the live renderer to this path")
	command.Flags().StringVar(&options.evaluate, "eval", "", "JavaScript expression to evaluate in the live renderer")
	command.Flags().StringVar(&options.setFile, "set-file", "", "attach this file to the first enabled file input in the live renderer")
	command.Flags().StringVar(&options.endpoint, "endpoint", "", "explicit loopback CDP origin of a controlled renderer")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		return asExit(runDevInspect(cmd.Context(), options, stdout, stderr))
	}
	return command
}

func runDevInspect(ctx context.Context, options devInspectOptions, stdout, stderr io.Writer) int {
	repository, baseDir, endpoint := &options.repository, &options.baseDir, &options.endpoint
	screenshot, evaluate, setFile := &options.screenshot, &options.evaluate, &options.setFile
	if *screenshot == "" && *evaluate == "" && *setFile == "" {
		fmt.Fprintln(stderr, "dev inspect requires --screenshot, --eval, and/or --set-file")
		return 2
	}
	requestContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cdp, resolvedEndpoint, err := connectDevRenderer(requestContext, *repository, *baseDir, *endpoint, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "dev inspect: %v\n", err)
		return 1
	}
	defer cdp.Close()
	result := map[string]any{"schema": 1, "endpoint": resolvedEndpoint}
	if *setFile != "" {
		filename, absErr := filepath.Abs(*setFile)
		if absErr != nil {
			fmt.Fprintln(stderr, absErr)
			return 1
		}
		if err := setDevFileInput(requestContext, cdp, filename); err != nil {
			fmt.Fprintf(stderr, "attach file to development renderer: %v\n", err)
			return 1
		}
		result["setFile"] = filename
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
	return writeOutput(stdout, stderr, result)
}

func setDevFileInput(ctx context.Context, cdp *businessacceptance.CDPClient, filename string) error {
	var document struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := cdp.Call(ctx, "DOM.getDocument", map[string]any{"depth": 1}, &document); err != nil {
		return err
	}
	var query struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := cdp.Call(ctx, "DOM.querySelector", map[string]any{
		"nodeId": document.Root.NodeID, "selector": `input[type="file"]:not(:disabled)`,
	}, &query); err != nil {
		return err
	}
	if query.NodeID == 0 {
		return fmt.Errorf("the renderer does not expose an enabled file input")
	}
	return cdp.Call(ctx, "DOM.setFileInputFiles", map[string]any{
		"nodeId": query.NodeID, "files": []string{filename},
	}, nil)
}
