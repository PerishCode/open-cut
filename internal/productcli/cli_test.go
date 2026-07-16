package productcli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/internal/testfixture"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestResolveAndInspectActiveCellThroughObserverCapability(t *testing.T) {
	root := t.TempDir()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "cli", ProtocolFloor: "bootstrap.v1",
		DataDir:      filepath.Join(root, "data", "beta", "cli"),
		Installation: testfixture.InstallationAssertion(),
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"),
			LogRoot: filepath.Join(root, "logs"),
		},
		InitialTrustRoot: config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{
			ID: "test", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
		}}},
	}
	bootstrapPath := filepath.Join(bootstrap.Roots.BootstrapRoot, "bootstrap.json")
	if err := atomicfile.WriteJSON(bootstrapPath, bootstrap, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	active := "1.0.0-beta.1"
	candidate := "2.0.0-beta.1"
	if err := state.Save(paths.StateFile, identity.Channel, state.Runtime{
		Schema: state.Schema, Generation: 9, Active: active, LastGood: active, Candidate: candidate, Attempt: 2,
	}); err != nil {
		t.Fatal(err)
	}
	versionRoot := filepath.Join(paths.Versions, active)
	manifest := release.Manifest{
		Schema: release.ManifestSchema, Channel: identity.Channel, Version: active,
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Launcher: release.Entry{Entry: "launcher/launcher"}, Payload: release.Entry{Entry: "payload/runtime-topology.json"},
		MinimumBootstrapProtocol: bootstrap.ProtocolFloor, PublishedAt: time.Now().UTC(),
	}
	if err := atomicfile.WriteJSON(filepath.Join(versionRoot, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	cliEntry, _ := release.CLIEntry(target.Host())
	cliPath := filepath.Join(versionRoot, filepath.FromSlash(cliEntry))
	if err := os.MkdirAll(filepath.Dir(cliPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cliPath, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveActiveCLI(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Active != active || resolution.CLIExecutable != cliPath {
		t.Fatalf("resolution = %#v", resolution)
	}

	cellBroker, err := broker.Start(broker.Options{
		Identity: identity, Paths: paths, Generation: 9, OwnerTokenTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"status", "--bootstrap", bootstrapPath}, Options{
		Stdout: &stdout, Stderr: &stderr,
	}); code != 0 {
		t.Fatalf("Run() = %d, stderr=%s", code, stderr.String())
	}
	var result Status
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Active != active || result.Cell.Channel != identity.Channel || result.Cell.Namespace != identity.Namespace {
		t.Fatalf("status = %#v", result)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{
		"__active", "--bootstrap", bootstrapPath, "project", "show", "--help",
	}, Options{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("active help = %d, stderr=%s", code, stderr.String())
	}
	var discovery command.Discovery
	if err := json.Unmarshal(stdout.Bytes(), &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery.Schema != command.HelpSchemaVersion || discovery.CLIVersion != active ||
		len(discovery.Path) != 2 || discovery.Path[0] != "project" || discovery.Path[1] != "show" ||
		discovery.Input == nil || discovery.Result == nil {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestActiveBusinessReadUsesObserverDiscoveryAndHiddenLifecycleSignature(t *testing.T) {
	root := t.TempDir()
	installation, err := lifecycle.EnsureDevelopmentInstallationIdentity(
		filepath.Join(root, "identity"), []string{authwire.CLIRole, "first-party-ui"},
	)
	if err != nil {
		t.Fatal(err)
	}
	trustPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "business-cli", ProtocolFloor: "bootstrap.v1",
		DataDir: filepath.Join(root, "data"), Installation: installation.Assertion(),
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"),
			LogRoot: filepath.Join(root, "logs"),
		},
		InitialTrustRoot: config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{
			ID: "test", PublicKey: base64.StdEncoding.EncodeToString(trustPublic),
		}}},
	}
	bootstrapPath := filepath.Join(bootstrap.Roots.BootstrapRoot, "bootstrap.json")
	if err := atomicfile.WriteJSON(bootstrapPath, bootstrap, 0o600); err != nil {
		t.Fatal(err)
	}
	cellIdentity, _ := cell.New(bootstrap.Channel, bootstrap.Namespace)
	paths, err := layout.Resolve(bootstrap.Roots, cellIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(paths.StateFile, bootstrap.Channel, state.Runtime{
		Schema: state.Schema, Generation: 4, Active: "0.1.0-beta.1", LastGood: "0.1.0-beta.1",
	}); err != nil {
		t.Fatal(err)
	}
	cellBroker, err := broker.Start(broker.Options{
		Identity: cellIdentity, Paths: paths, Generation: 4, HeartbeatTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()

	fingerprint, err := command.InitialRegistry().Fingerprint([]string{"project", "list"})
	if err != nil {
		t.Fatal(err)
	}
	apiInstance, err := domain.GenerateUUIDv7(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	policyRevision, _ := domain.NewRevision(1)
	grantRevision, _ := domain.NewRevision(1)
	grantScopeDigest, _ := application.CLIScopeDigest([]string{
		string(command.ScopeActivityRead), string(command.ScopeProjectRead),
	})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case authwire.CLIChallengeRoute:
			var input authwire.CLIChallengeRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				http.Error(response, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			if input.CommandFingerprint != fingerprint || input.Path != "/v1/projects" {
				http.Error(response, "binding mismatch", http.StatusUnprocessableEntity)
				return
			}
			policy, err := application.NewInvocationPolicySnapshot(application.InvocationPolicySettings{
				Revision: policyRevision, Policy: application.DefaultInvocationPolicy(),
			}, input.PolicyOverride)
			if err != nil {
				http.Error(response, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			invocationID, _ := domain.ParseCommandReceiptID("018f0000-0000-7000-8000-000000000298")
			inputDigest, err := invocationDigest(businessInvocation{
				name: input.Command, method: input.Method, path: input.Path, query: input.Query,
				bodyDigest: input.BodyDigest, context: input.Context, requestID: input.RequestID,
				fingerprint: input.CommandFingerprint,
			})
			if err != nil {
				http.Error(response, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			_ = json.NewEncoder(response).Encode(authwire.CLIChallengeResult{
				Schema: authwire.CLIChallengeSchema, InvocationID: invocationID, GrantID: apiInstance,
				GrantRevision: &grantRevision, GrantScopeDigest: grantScopeDigest.String(),
				Nonce: strings.Repeat("n", 43), ExpiresAt: time.Now().Add(time.Minute),
				InstallationID:         bootstrap.Installation.InstallationID,
				InstallationGeneration: bootstrap.Installation.Generation,
				CellGeneration:         4, APIInstanceID: apiInstance, ClientInstance: input.ClientInstance,
				Command: input.Command, CommandFingerprint: input.CommandFingerprint,
				RequiredScope: string(command.ScopeProjectRead), Method: input.Method, Path: input.Path,
				Query: input.Query, BodyDigest: input.BodyDigest, InputDigest: inputDigest,
				RequestID: input.RequestID, Receipt: application.CommandReceiptNone, Context: input.Context,
				Policy: policy, Role: authwire.CLIRole,
				SigningPayload: base64.RawURLEncoding.EncodeToString([]byte("signed CLI fixture")),
			})
		case "/v1/projects":
			if request.Header.Get(authwire.HeaderGrant) != apiInstance ||
				request.Header.Get(authwire.HeaderChallenge) == "" || request.Header.Get(authwire.HeaderSignature) == "" {
				http.Error(response, "signature headers missing", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(response).Encode(application.ListProjectsResult{
				Projects: []application.ProjectSummary{}, ActivityCursor: 0,
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	apiToken, err := cellBroker.MintSidecarToken("api", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	apiSession, err := client.DialSession(context.Background(), cellBroker.Descriptor(), apiToken, client.Registration{
		Channel: bootstrap.Channel, Namespace: bootstrap.Namespace, App: "api",
		Mode: protocol.LifecycleModeHarness, Source: "harness",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer apiSession.Close(0)
	if err := apiSession.Endpoint("http", server.URL); err != nil {
		t.Fatal(err)
	}
	if err := apiSession.Ready(); err != nil {
		t.Fatal(err)
	}
	signerRoot, err := os.MkdirTemp("", "oc-cli-signer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(signerRoot)
	signer, err := lifecycle.StartDevelopmentSigner(filepath.Join(signerRoot, "signer.sock"), installation)
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	t.Setenv(lifecycle.SignerSocketEnvironment, signer.Socket())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"__active", "--bootstrap", bootstrapPath, "project", "list",
	}, Options{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var result command.Result[application.ListProjectsResult]
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != command.StatusSucceeded || result.Data == nil || result.ActivityCursor == nil ||
		result.ActivityCursor.String() != "0" || strings.Contains(stdout.String(), "signature") {
		t.Fatalf("result=%+v stdout=%s", result, stdout.String())
	}
}

func TestStableResolverPinsPlatformSignerAndDropsCallerOverrides(t *testing.T) {
	environment := activeCLIEnvironment([]string{
		"PATH=/usr/bin",
		lifecycle.PlatformHostEnvironment + "=/tmp/untrusted-host",
		lifecycle.SignerSocketEnvironment + "=/tmp/untrusted-signer.sock",
	}, "/Applications/Open Cut.app/Contents/MacOS/Open Cut")
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "untrusted") || strings.Contains(joined, lifecycle.SignerSocketEnvironment+"=") ||
		!strings.Contains(joined, lifecycle.PlatformHostEnvironment+"=/Applications/Open Cut.app/Contents/MacOS/Open Cut") ||
		!strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatalf("active CLI environment=%q", environment)
	}
}

func TestBusinessReadinessUsesObserverWithoutLifecycleMutation(t *testing.T) {
	root := t.TempDir()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := config.Bootstrap{
		Schema: 1, Channel: "beta", Namespace: "cli-readiness", DataDir: filepath.Join(root, "data"),
		ProtocolFloor: "bootstrap.v1", Installation: testfixture.InstallationAssertion(),
		InitialTrustRoot: config.TrustConfig{Threshold: 1, Keys: []config.TrustKey{{
			ID: "test", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
		}}},
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"), StoreRoot: filepath.Join(root, "store"),
			CacheRoot: filepath.Join(root, "cache"), RuntimeRoot: filepath.Join(root, "runtime"),
			LogRoot: filepath.Join(root, "logs"),
		},
	}
	bootstrapPath := filepath.Join(bootstrap.Roots.BootstrapRoot, "bootstrap.json")
	if err := atomicfile.WriteJSON(bootstrapPath, bootstrap, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := cell.New(bootstrap.Channel, bootstrap.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	defer cellBroker.Close()
	apiToken, err := cellBroker.MintSidecarToken("api", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	api, err := client.DialSession(context.Background(), cellBroker.Descriptor(), apiToken, client.Registration{
		Channel: bootstrap.Channel, Namespace: bootstrap.Namespace, App: "api",
		Mode: protocol.LifecycleModeHarness, Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close(0)
	if err := api.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := ensureBusinessReady(context.Background(), install.Receipt{
		BootstrapPath: bootstrapPath, HostPath: filepath.Join(root, "must-not-start"), InstallRoot: root,
	}, time.Second); err != nil {
		t.Fatal(err)
	}
}
