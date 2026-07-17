package tests

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestCLIRequiresPossessionThenExactCreatorApprovedGrant(t *testing.T) {
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)}
	ui, uiPrivateKey := newTestUISessions(t, store, clock, false)
	cli, cliPrivateKey := newTestCLIAuthorization(t, store, clock)
	authorizer := service.CombinedAuthorizer{UI: ui, CLI: cli}
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, authorizer,
	)
	server := httptest.NewServer(mux)
	defer server.Close()

	fingerprint, err := command.InitialRegistry().Fingerprint([]string{"project", "list"})
	if err != nil {
		t.Fatal(err)
	}
	waitMilliseconds := uint32(1500)
	challengeRequest := service.CLIChallengeRequest{
		ClientInstance: "cli-http-1", Command: "project list", CommandFingerprint: fingerprint,
		Method: http.MethodGet, Path: "/v1/projects", BodyDigest: service.NoBodyDigest("project list"),
		PolicyOverride: application.InvocationPolicyOverride{WaitMilliseconds: &waitMilliseconds},
	}
	challenge := requestCLIChallenge(t, server, challengeRequest)
	if challenge.GrantID != "" {
		t.Fatalf("unknown CLI challenge disclosed unexpected grant %q", challenge.GrantID)
	}
	if challenge.Policy.SettingsRevision.Value() != 1 ||
		challenge.Policy.Persisted != application.DefaultInvocationPolicy() ||
		challenge.Policy.Effective.WaitMilliseconds != waitMilliseconds {
		t.Fatalf("policy snapshot=%+v", challenge.Policy)
	}
	request := httptest.NewRequest(http.MethodGet, server.URL+"/v1/projects", nil)
	request.Header.Set("X-Open-Cut-CLI-Challenge", challenge.Nonce)
	request.Header.Set("X-Open-Cut-CLI-Signature", signCLIChallenge(t, cliPrivateKey, challenge))
	response := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("X-Open-Cut-CLI-Auth-Status") != "pairing-required" {
		t.Fatalf("pairing response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	pairingID := response.Header().Get("X-Open-Cut-CLI-Pairing-ID")
	if pairingID == "" {
		t.Fatal("pairing response omitted pending grant identity")
	}

	uiSession := issueTestUISession(t, ui, uiPrivateKey, "electron-cli-approval")
	listRequest := httptest.NewRequest(http.MethodGet, server.URL+"/v1/authorization/cli/pairings", nil)
	listRequest.Header.Set("X-Open-Cut-UI-Session", uiSession)
	listResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), "publicKey\"") {
		t.Fatalf("pairing list=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listed struct {
		Grants []application.CLIGrant `json:"grants"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Grants) != 1 || listed.Grants[0].ID != pairingID ||
		listed.Grants[0].Status != application.CLIGrantPending || len(listed.Grants[0].Scopes) != 10 {
		t.Fatalf("pairings=%+v", listed.Grants)
	}

	approve := httptest.NewRequest(
		http.MethodPost, server.URL+"/v1/authorization/cli/pairings/"+pairingID+"/approve", nil,
	)
	approve.Header.Set("X-Open-Cut-UI-Session", uiSession)
	approveResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(approveResponse, approve)
	if approveResponse.Code != http.StatusOK {
		t.Fatalf("approve=%d body=%s", approveResponse.Code, approveResponse.Body.String())
	}

	clock.now = clock.now.Add(time.Second)
	challengeRequest.ClientInstance = "cli-http-2"
	challenge = requestCLIChallenge(t, server, challengeRequest)
	if challenge.GrantID != pairingID {
		t.Fatalf("authorized challenge grant=%q want=%q", challenge.GrantID, pairingID)
	}
	request = httptest.NewRequest(http.MethodGet, server.URL+"/v1/projects", nil)
	request.Header.Set("X-Open-Cut-CLI-Grant", pairingID)
	request.Header.Set("X-Open-Cut-CLI-Challenge", challenge.Nonce)
	request.Header.Set("X-Open-Cut-CLI-Signature", signCLIChallenge(t, cliPrivateKey, challenge))
	response = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized read=%d body=%s", response.Code, response.Body.String())
	}
	revoke := httptest.NewRequest(
		http.MethodPost, server.URL+"/v1/authorization/cli/pairings/"+pairingID+"/revoke", nil,
	)
	revoke.Header.Set("X-Open-Cut-UI-Session", uiSession)
	revokeResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(revokeResponse, revoke)
	if revokeResponse.Code != http.StatusOK {
		t.Fatalf("revoke=%d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}

	clock.now = clock.now.Add(time.Second)
	challengeRequest.ClientInstance = "cli-http-3"
	challenge = requestCLIChallenge(t, server, challengeRequest)
	request = httptest.NewRequest(http.MethodGet, server.URL+"/v1/projects", nil)
	request.Header.Set("X-Open-Cut-CLI-Grant", pairingID)
	request.Header.Set("X-Open-Cut-CLI-Challenge", challenge.Nonce)
	request.Header.Set("X-Open-Cut-CLI-Signature", signCLIChallenge(t, cliPrivateKey, challenge))
	response = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("X-Open-Cut-CLI-Auth-Status") != "grant-revoked" {
		t.Fatalf("revoked read=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if len(store.AuthorizationAudits()) < 6 {
		t.Fatalf("authorization audit=%+v", store.AuthorizationAudits())
	}
}

// TestCLICommandBodyAuthorityCoversEveryPostBodyCommand proves that every
// Agent command the CLI sends with a JSON body authorizes through the router:
// a command whose route uses the no-body authority middleware recomputes the
// body digest as the empty-body sentinel and rejects the CLI's real digest,
// silently 401ing that command in installed products. The failure hides from
// CI because the fast lane stops before frames and exports.
func TestCLICommandBodyAuthorityCoversEveryPostBodyCommand(t *testing.T) {
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)}
	ui, uiPrivateKey := newTestUISessions(t, store, clock, false)
	cli, cliPrivateKey := newTestCLIAuthorization(t, store, clock)
	authorizer := service.CombinedAuthorizer{UI: ui, CLI: cli}
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, authorizer,
	)
	server := httptest.NewServer(mux)
	defer server.Close()

	project := "018f0000-0000-7000-8000-000000000001"
	sequence := "018f0000-0000-7000-8000-000000000002"
	asset := "018f0000-0000-7000-8000-000000000003"
	run := "018f0000-0000-7000-8000-000000000004"
	turn := "018f0000-0000-7000-8000-000000000005"
	job := "018f0000-0000-7000-8000-000000000006"
	prefix := "/v1/projects/" + project + "/runs/" + run + "/turns/" + turn
	projectID, err := domain.ParseProjectID(project)
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, err := domain.ParseSequenceID(sequence)
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.ParseRunID(run)
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := domain.ParseTurnID(turn)
	if err != nil {
		t.Fatal(err)
	}
	runContext := command.Context{ProjectID: &projectID, RunID: &runID, TurnID: &turnID}
	sequenceContext := command.Context{ProjectID: &projectID, SequenceID: &sequenceID, RunID: &runID, TurnID: &turnID}
	cases := []struct {
		command []string
		path    string
		body    string
		context command.Context
	}{
		{[]string{"asset", "frames"}, prefix + "/assets/" + asset + "/frames", `{"sourceStreamId":"` + sequence + `","times":[{"value":"0","scale":1}]}`, runContext},
		{[]string{"sequence", "frames"}, prefix + "/sequences/" + sequence + "/frames", `{"sequenceRevision":"1","times":[{"value":"0","scale":1}]}`, sequenceContext},
		{[]string{"export", "start"}, prefix + "/sequences/" + sequence + "/exports", `{"requestId":"journey-export","sequenceRevision":"1","preset":"webm-vp9-opus-v1"}`, sequenceContext},
		{[]string{"export", "retry"}, prefix + "/exports/" + job + "/retry", `{"jobId":"` + job + `"}`, runContext},
		{[]string{"export", "cancel"}, prefix + "/exports/" + job + "/cancel", `{"jobId":"` + job + `","requestId":"journey-cancel"}`, runContext},
	}
	registry := command.InitialRegistry()
	pairingApproved := false
	for index, testCase := range cases {
		name := strings.Join(testCase.command, " ")
		fingerprint, err := registry.Fingerprint(testCase.command)
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := registry.Lookup(testCase.command)
		if err != nil {
			t.Fatal(err)
		}
		bodyDigest, err := authwire.CommandBodyDigest(name, []byte(testCase.body))
		if err != nil {
			t.Fatal(err)
		}
		challengeRequest := service.CLIChallengeRequest{
			ClientInstance: "cli-body-" + strconv.Itoa(index), Command: name, CommandFingerprint: fingerprint,
			Method: http.MethodPost, Path: testCase.path, BodyDigest: bodyDigest.String(), Context: testCase.context,
		}
		if descriptor.RequestIdentity {
			challengeRequest.RequestID = "journey-body-" + strconv.Itoa(index)
		}
		clock.now = clock.now.Add(time.Second)
		challenge := requestCLIChallenge(t, server, challengeRequest)
		grantID := challenge.GrantID
		request := httptest.NewRequest(http.MethodPost, server.URL+testCase.path, strings.NewReader(testCase.body))
		request.Header.Set("Content-Type", "application/json")
		if grantID != "" {
			request.Header.Set("X-Open-Cut-CLI-Grant", grantID)
		}
		request.Header.Set("X-Open-Cut-CLI-Challenge", challenge.Nonce)
		request.Header.Set("X-Open-Cut-CLI-Signature", signCLIChallenge(t, cliPrivateKey, challenge))
		response := httptest.NewRecorder()
		server.Config.Handler.ServeHTTP(response, request)

		if !pairingApproved {
			// The first command establishes and approves the durable grant so
			// the remaining commands exercise authorized body digests.
			if response.Code != http.StatusUnauthorized ||
				response.Header().Get("X-Open-Cut-CLI-Auth-Status") != "pairing-required" {
				t.Fatalf("%s expected pairing-required, got %d %s", name, response.Code, response.Body.String())
			}
			pairingID := response.Header().Get("X-Open-Cut-CLI-Pairing-ID")
			uiSession := issueTestUISession(t, ui, uiPrivateKey, "electron-body-approval")
			approve := httptest.NewRequest(
				http.MethodPost, server.URL+"/v1/authorization/cli/pairings/"+pairingID+"/approve", nil,
			)
			approve.Header.Set("X-Open-Cut-UI-Session", uiSession)
			approveResponse := httptest.NewRecorder()
			server.Config.Handler.ServeHTTP(approveResponse, approve)
			if approveResponse.Code != http.StatusOK {
				t.Fatalf("approve=%d body=%s", approveResponse.Code, approveResponse.Body.String())
			}
			pairingApproved = true
			continue
		}
		if response.Code == http.StatusUnauthorized && strings.Contains(response.Body.String(), "product authority required") {
			t.Fatalf(
				"%s POST body command failed CLI body authorization: %d %s",
				name, response.Code, response.Body.String(),
			)
		}
	}
}

func TestCLIChallengeRejectsBindingChangesAndConsumesNonce(t *testing.T) {
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)}
	cli, privateKey := newTestCLIAuthorization(t, store, clock)
	fingerprint, err := command.InitialRegistry().Fingerprint([]string{"activity", "list"})
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := cli.ChallengeCLI(context.Background(), service.CLIChallengeRequest{
		ClientInstance: "cli-binding-1", Command: "activity list", CommandFingerprint: fingerprint,
		Method: http.MethodGet, Path: "/v1/activity", Query: "after=0", BodyDigest: service.NoBodyDigest("activity list"),
	})
	if err != nil {
		t.Fatal(err)
	}
	signature := signCLIChallenge(t, privateKey, challenge)
	_, err = cli.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodGet, Path: "/v1/activity", Query: "after=1", BodyDigest: service.NoBodyDigest("activity list"),
		Command: "activity list", CommandFingerprint: fingerprint, RequiredScope: string(command.ScopeActivityRead),
		CLIChallenge: challenge.Nonce, CLISignature: signature,
	})
	if !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("changed query error=%v", err)
	}
	_, err = cli.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodGet, Path: "/v1/activity", Query: "after=0", BodyDigest: service.NoBodyDigest("activity list"),
		Command: "activity list", CommandFingerprint: fingerprint, RequiredScope: string(command.ScopeActivityRead),
		CLIChallenge: challenge.Nonce, CLISignature: signature,
	})
	if !errors.Is(err, service.ErrCLIChallengeInvalid) {
		t.Fatalf("replayed challenge error=%v", err)
	}
	grants, err := store.ListCLIGrants(context.Background(), "installation-ui-test")
	if err != nil || len(grants) != 0 {
		t.Fatalf("tampered proof created grants=%+v err=%v", grants, err)
	}
}

func TestCLIChallengeAcceptsSpecializedHTTPBindings(t *testing.T) {
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)}
	cli, _ := newTestCLIAuthorization(t, store, clock)
	projectID, err := domain.ParseProjectID("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, err := domain.ParseSequenceID("018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	assetID, err := domain.ParseAssetID("018f0000-0000-7000-8000-000000000003")
	if err != nil {
		t.Fatal(err)
	}
	projectContext := command.Context{ProjectID: &projectID}
	sequenceContext := command.Context{ProjectID: &projectID, SequenceID: &sequenceID}
	bindings := []struct {
		name    string
		method  string
		path    string
		query   string
		context command.Context
		body    []byte
	}{
		{name: "product status", method: http.MethodGet, path: "/v1/product/status"},
		{
			name: "transcript read", method: http.MethodGet,
			path:  "/v1/projects/" + projectID.String() + "/assets/" + assetID.String() + "/transcript",
			query: "limit=20", context: projectContext,
		},
		{name: "entity show", method: http.MethodGet, path: "/v1/projects/" + projectID.String() + "/entities/narrative-node/018f0000-0000-7000-8000-000000000010", context: projectContext},
		{name: "entity show", method: http.MethodGet, path: "/v1/projects/" + projectID.String() + "/entities/transcript-correction/018f0000-0000-7000-8000-000000000011", context: projectContext},
		{name: "entity show", method: http.MethodGet, path: "/v1/projects/" + projectID.String() + "/entities/caption/018f0000-0000-7000-8000-000000000012", context: projectContext},
		{name: "entity show", method: http.MethodGet, path: "/v1/projects/" + projectID.String() + "/entities/alignment/018f0000-0000-7000-8000-000000000013", context: projectContext},
		{name: "entity show", method: http.MethodGet, path: "/v1/projects/" + projectID.String() + "/entities/clip/018f0000-0000-7000-8000-000000000014", context: projectContext},
		{name: "entity show", method: http.MethodGet, path: "/v1/projects/" + projectID.String() + "/entities/link-group/018f0000-0000-7000-8000-000000000015", context: projectContext},
		{
			name: "edit derive-captions", method: http.MethodGet,
			path:    "/v1/projects/" + projectID.String() + "/sequences/" + sequenceID.String() + "/edit/caption-derivation",
			context: sequenceContext,
		},
		{
			name: "edit derive-rough-cut", method: http.MethodPost,
			path:    "/v1/projects/" + projectID.String() + "/sequences/" + sequenceID.String() + "/edit/rough-cut-derivation",
			context: sequenceContext, body: []byte("{}"),
		},
	}
	registry := command.InitialRegistry()
	for index, binding := range bindings {
		fingerprint, fingerprintErr := registry.Fingerprint(strings.Fields(binding.name))
		if fingerprintErr != nil {
			t.Fatal(fingerprintErr)
		}
		bodyDigest := service.NoBodyDigest(binding.name)
		if binding.body != nil {
			digest, digestErr := authwire.CommandBodyDigest(binding.name, binding.body)
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			bodyDigest = digest.String()
		}
		challenge, challengeErr := cli.ChallengeCLI(context.Background(), service.CLIChallengeRequest{
			ClientInstance: "cli-binding-matrix-" + strconv.Itoa(index),
			Command:        binding.name, CommandFingerprint: fingerprint,
			Method: binding.method, Path: binding.path, Query: binding.query,
			BodyDigest: bodyDigest, Context: binding.context,
		})
		if challengeErr != nil || challenge.Command != binding.name || challenge.Method != binding.method {
			t.Fatalf("binding=%+v challenge=%+v err=%v", binding, challenge, challengeErr)
		}
	}
}

func TestCLIGrantRevisionInvalidatesOldChallengeWithoutChangingAgentPrincipal(t *testing.T) {
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)}
	cli, privateKey := newTestCLIAuthorization(t, store, clock)
	fingerprint, err := command.InitialRegistry().Fingerprint([]string{"project", "list"})
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyDigest := sha256.Sum256(publicKey)
	grantID, _ := domain.GenerateUUIDv7(clock.now)
	agentValue, _ := domain.GenerateUUIDv7(clock.now.Add(time.Millisecond))
	agentID, _ := domain.ParseAgentID(agentValue)
	pending, err := store.EnsurePendingCLIGrant(context.Background(), application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-ui-test", AgentID: agentID,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKey),
		Fingerprint: "sha256:" + hex.EncodeToString(keyDigest[:]), Scopes: []string{"project:read"},
		CreatedAt: clock.now, ExpiresAt: clock.now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := store.DecideCLIGrant(context.Background(), pending.ID, true, clock.now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	challengeRequest := service.CLIChallengeRequest{
		ClientInstance: "cli-revision-old", Command: "project list", CommandFingerprint: fingerprint,
		Method: http.MethodGet, Path: "/v1/projects", BodyDigest: service.NoBodyDigest("project list"),
	}

	clock.now = clock.now.Add(2 * time.Second)
	oldChallenge, err := cli.ChallengeCLI(context.Background(), challengeRequest)
	if err != nil {
		t.Fatal(err)
	}
	upgradeID, _ := domain.GenerateUUIDv7(clock.now)
	upgrade, err := store.EnsurePendingCLIGrantScopeUpgrade(context.Background(), application.PendingCLIGrantScopeUpgrade{
		ID: upgradeID, GrantID: grant.ID, FromRevision: grant.Revision,
		RequestedScopes: append(append([]string(nil), grant.Scopes...), "edit:read"),
		CreatedAt:       clock.now, ExpiresAt: clock.now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, upgraded, err := store.DecideCLIGrantScopeUpgrade(context.Background(), upgrade.ID, true, clock.now.Add(time.Second))
	if err != nil || upgraded.Revision.Value() != 2 || upgraded.AgentID != grant.AgentID {
		t.Fatalf("upgraded=%+v err=%v", upgraded, err)
	}
	_, err = cli.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodGet, Path: "/v1/projects", BodyDigest: service.NoBodyDigest("project list"),
		Command: "project list", CommandFingerprint: fingerprint, RequiredScope: string(command.ScopeProjectRead),
		CLIGrant: grant.ID, CLIChallenge: oldChallenge.Nonce,
		CLISignature: signCLIChallenge(t, privateKey, oldChallenge),
	})
	if !errors.Is(err, service.ErrCLIGrantAuthorityChanged) {
		t.Fatalf("old challenge error=%v", err)
	}

	clock.now = clock.now.Add(2 * time.Second)
	challengeRequest.ClientInstance = "cli-revision-new"
	newChallenge, err := cli.ChallengeCLI(context.Background(), challengeRequest)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := cli.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodGet, Path: "/v1/projects", BodyDigest: service.NoBodyDigest("project list"),
		Command: "project list", CommandFingerprint: fingerprint, RequiredScope: string(command.ScopeProjectRead),
		CLIGrant: grant.ID, CLIChallenge: newChallenge.Nonce,
		CLISignature: signCLIChallenge(t, privateKey, newChallenge),
	})
	if err != nil || authority.Actor.IDString() != grant.AgentID.String() ||
		authority.Policy != newChallenge.Policy.Effective {
		t.Fatalf("authority=%+v err=%v", authority, err)
	}
}

func TestSignedRunBodyRequiresScopeUpgradeAndRejectsTampering(t *testing.T) {
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)}
	cli, privateKey := newTestCLIAuthorization(t, store, clock)
	projects, reads, activity, runs := testProjectApplications(t, store)
	projectRequest, _ := domain.ParseRequestID("gesture:create-signed-run")
	project, err := projects.Create(creatorContext(t), application.CreateProjectInput{
		RequestID: projectRequest, Name: "Signed body",
	})
	if err != nil {
		t.Fatal(err)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	encodedKey := base64.StdEncoding.EncodeToString(publicKey)
	fingerprintValue := sha256.Sum256(publicKey)
	grantID, _ := domain.GenerateUUIDv7(clock.now)
	agentValue, _ := domain.GenerateUUIDv7(clock.now.Add(time.Millisecond))
	agentID, _ := domain.ParseAgentID(agentValue)
	grant, err := store.EnsurePendingCLIGrant(context.Background(), application.PendingCLIGrant{
		ID: grantID, InstallationID: "installation-ui-test", AgentID: agentID,
		PublicKey: encodedKey, Fingerprint: "sha256:" + hex.EncodeToString(fingerprintValue[:]),
		Scopes: []string{"activity:read", "project:read"}, CreatedAt: clock.now,
		ExpiresAt: clock.now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideCLIGrant(context.Background(), grant.ID, true, clock.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	authorizer := service.CombinedAuthorizer{CLI: cli}
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, authorizer,
	)
	server := httptest.NewServer(mux)
	defer server.Close()

	requestID, _ := domain.ParseRequestID("agent:run:signed:001")
	body, err := json.Marshal(application.RunBeginInput{RequestID: requestID, Intent: "Create the first cut"})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := authwire.CommandBodyDigest("run begin", body)
	if err != nil {
		t.Fatal(err)
	}
	commandFingerprint, err := command.InitialRegistry().Fingerprint([]string{"run", "begin"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/projects/" + project.Project.Project.ID.String() + "/runs"
	commandContext := command.Context{ProjectID: &project.Project.Project.ID}
	challengeRequest := service.CLIChallengeRequest{
		ClientInstance: "cli-run-upgrade", Command: "run begin", CommandFingerprint: commandFingerprint,
		Method: http.MethodPost, Path: path, BodyDigest: digest.String(), RequestID: requestID.String(),
		Context: commandContext,
	}
	challenge := requestCLIChallenge(t, server, challengeRequest)
	response := signedBusinessRequest(t, server, path, body, challenge, privateKey)
	if response.Code != http.StatusForbidden ||
		response.Header().Get(authwire.HeaderAuthStatus) != authwire.AuthStatusScopeUpgradeRequired {
		t.Fatalf("upgrade response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	upgradeID := response.Header().Get(authwire.HeaderScopeUpgradeID)
	upgrades, err := store.ListCLIGrantScopeUpgrades(context.Background(), "installation-ui-test")
	if err != nil || len(upgrades) != 1 || upgrades[0].ID != upgradeID ||
		!strings.Contains(strings.Join(upgrades[0].RequestedScopes, ","), "run:write") {
		t.Fatalf("upgrades=%+v err=%v", upgrades, err)
	}
	if _, upgraded, err := store.DecideCLIGrantScopeUpgrade(
		context.Background(), upgradeID, true, clock.now.Add(2*time.Second),
	); err != nil || upgraded.Revision.Value() != 2 || upgraded.AgentID != grant.AgentID {
		t.Fatalf("upgraded=%+v err=%v", upgraded, err)
	}

	clock.now = clock.now.Add(3 * time.Second)
	challengeRequest.ClientInstance = "cli-run-tampered"
	challenge = requestCLIChallenge(t, server, challengeRequest)
	tampered, _ := json.Marshal(application.RunBeginInput{RequestID: requestID, Intent: "Tampered intent"})
	response = signedBusinessRequest(t, server, path, tampered, challenge, privateKey)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("tampered response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	response = signedBusinessRequest(t, server, path, body, challenge, privateKey)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("replayed tampered challenge=%d body=%s", response.Code, response.Body.String())
	}

	clock.now = clock.now.Add(time.Second)
	challengeRequest.ClientInstance = "cli-run-authorized"
	challenge = requestCLIChallenge(t, server, challengeRequest)
	response = signedBusinessRequest(t, server, path, body, challenge, privateKey)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized run=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	var result application.RunCommandResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result.Run.ProjectID != project.Project.Project.ID ||
		result.Run.Status != application.AgentRunActive || result.Run.CurrentTurn.Generation.Value() != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func signedBusinessRequest(
	t *testing.T,
	server *httptest.Server,
	path string,
	body []byte,
	challenge service.CLIChallengeResult,
	privateKey ed25519.PrivateKey,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(authwire.HeaderGrant, challenge.GrantID)
	request.Header.Set(authwire.HeaderChallenge, challenge.Nonce)
	request.Header.Set(authwire.HeaderSignature, signCLIChallenge(t, privateKey, challenge))
	response := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(response, request)
	return response
}

func newTestCLIAuthorization(
	t *testing.T,
	store application.AuthorizationRepository,
	clock application.Clock,
) (*service.CLIAuthorizationService, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := service.NewCLIAuthorizationService(context.Background(), service.CLIChallengeConfig{
		InstallationID: "installation-ui-test", InstallationGeneration: 1, CellGeneration: 7,
		PublicKey: publicKey,
	}, store, nil, application.UUIDv7IdentityGenerator{}, clock, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return cli, privateKey
}

func requestCLIChallenge(
	t *testing.T,
	server *httptest.Server,
	request service.CLIChallengeRequest,
) service.CLIChallengeResult {
	t.Helper()
	response := postJSON(t, server, "/v1/auth/cli/challenges", request, "")
	if response.Code != http.StatusOK {
		t.Fatalf("CLI challenge=%d body=%s", response.Code, response.Body.String())
	}
	var result service.CLIChallengeResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func signCLIChallenge(t *testing.T, key ed25519.PrivateKey, challenge service.CLIChallengeResult) string {
	t.Helper()
	payload, err := base64.RawURLEncoding.DecodeString(challenge.SigningPayload)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))
}
