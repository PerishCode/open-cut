package productcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const (
	AgentAdapterContextSchema   = 1
	agentAdapterContextFilename = ".open-cut-agent-context.json"
)

// AgentAdapterContext is private resolver state. It lives beside the
// adapter-owned temporary executable and is never added to the Agent
// environment, prompt, command help, or command results.
type AgentAdapterContext struct {
	Schema       int    `json:"schema"`
	Endpoint     string `json:"endpoint"`
	SignerSocket string `json:"signerSocket"`
	CLIVersion   string `json:"cliVersion"`
}

func WriteAgentAdapterContext(executable string, value AgentAdapterContext) error {
	if !agentAdapterExecutable(executable) || !validAgentAdapterContext(value) {
		return fmt.Errorf("invalid Agent adapter CLI context")
	}
	return atomicfile.WriteJSON(
		filepath.Join(filepath.Dir(executable), agentAdapterContextFilename),
		value,
		0o600,
	)
}

func IsAgentAdapterExecutable(executable string) bool {
	return agentAdapterExecutable(executable)
}

func RunAgentAdapter(ctx context.Context, args []string, options Options) int {
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Stdin == nil {
		options.Stdin = strings.NewReader("")
	}
	if !agentAdapterExecutable(options.Executable) {
		fmt.Fprintln(options.Stderr, "temporary product resolver is unavailable")
		return 1
	}
	value, err := loadAgentAdapterContext(options.Executable)
	if err != nil {
		fmt.Fprintln(options.Stderr, "temporary product resolver is unavailable")
		return 1
	}
	if len(args) == 0 || isHelpInvocation(args) || (len(args) == 1 && args[0] == "help") {
		if len(args) == 0 || args[0] == "help" {
			args = []string{"--help"}
		}
		path := append([]string(nil), args[:len(args)-1]...)
		return writeDiscoveryForVersion(value.CLIVersion, path, options.Stdout, options.Stderr)
	}
	restoreSigner := pinAgentAdapterSigner(value.SignerSocket)
	defer restoreSigner()
	return runBusinessAtEndpoint(
		ctx,
		value.Endpoint,
		value.CLIVersion,
		args,
		options.Stdin,
		options.Stdout,
		options.Stderr,
	)
}

func runBusinessAtEndpoint(
	ctx context.Context,
	endpoint, cliVersion string,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	invocation, err := parseBusinessInvocation(args, stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := validateLoopbackEndpoint(endpoint); err != nil {
		return writeCommandFailure(
			stdout, stderr, cliVersion, invocation,
			command.StatusUnavailable, "product-unavailable", err, "",
		)
	}
	return runBusinessInvocation(ctx, endpoint, cliVersion, invocation, stdout, stderr)
}

func loadAgentAdapterContext(executable string) (AgentAdapterContext, error) {
	filename := filepath.Join(filepath.Dir(executable), agentAdapterContextFilename)
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() ||
		(runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return AgentAdapterContext{}, fmt.Errorf("invalid Agent adapter CLI context")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return AgentAdapterContext{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var value AgentAdapterContext
	if err := decoder.Decode(&value); err != nil || !validAgentAdapterContext(value) {
		return AgentAdapterContext{}, fmt.Errorf("invalid Agent adapter CLI context")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return AgentAdapterContext{}, fmt.Errorf("invalid Agent adapter CLI context")
	}
	return value, nil
}

func validAgentAdapterContext(value AgentAdapterContext) bool {
	return value.Schema == AgentAdapterContextSchema &&
		validateLoopbackEndpoint(value.Endpoint) == nil &&
		value.SignerSocket != "" && len(value.SignerSocket) <= 4096 &&
		!strings.ContainsRune(value.SignerSocket, 0) &&
		value.CLIVersion != "" && len(value.CLIVersion) <= 128 &&
		!strings.ContainsRune(value.CLIVersion, 0)
}

func agentAdapterExecutable(executable string) bool {
	if executable == "" || !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return false
	}
	name := filepath.Base(executable)
	return name == "open-cut" || name == "open-cut.exe"
}

func pinAgentAdapterSigner(socket string) func() {
	previousSocket, hadSocket := os.LookupEnv(lifecycle.SignerSocketEnvironment)
	previousHost, hadHost := os.LookupEnv(lifecycle.PlatformHostEnvironment)
	_ = os.Setenv(lifecycle.SignerSocketEnvironment, socket)
	_ = os.Unsetenv(lifecycle.PlatformHostEnvironment)
	return func() {
		restoreEnvironment(lifecycle.SignerSocketEnvironment, previousSocket, hadSocket)
		restoreEnvironment(lifecycle.PlatformHostEnvironment, previousHost, hadHost)
	}
}

func restoreEnvironment(name, value string, existed bool) {
	if existed {
		_ = os.Setenv(name, value)
		return
	}
	_ = os.Unsetenv(name)
}

func writeDiscoveryForVersion(cliVersion string, path []string, stdout, stderr io.Writer) int {
	discovery, err := command.InitialRegistry().Discover(path, cliVersion)
	if err != nil {
		fmt.Fprintf(stderr, "discover command: %v\n", err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(discovery); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
