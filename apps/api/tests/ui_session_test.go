package tests

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
)

type mutableClock struct {
	now time.Time
}

func (clock *mutableClock) Now() time.Time {
	return clock.now
}

func TestUISessionRequiresSingleUsePossessionProofAndAuditsAuthority(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)}
	sessions, privateKey := newTestUISessions(t, store, clock, false)
	challenge, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: "electron-instance-1", Origin: "oc://app",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: signUIChallenge(t, privateKey, challenge),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Schema != service.UISessionSchema || !result.ExpiresAt.Equal(clock.now.Add(service.UISessionTTL)) {
		t.Fatalf("session=%+v", result)
	}
	authority, err := sessions.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodGet, Route: "/v1/projects", UISession: result.Session,
	})
	if err != nil || authority.InstallationID != "installation-ui-test" || authority.Actor.CreatorID == nil {
		t.Fatalf("authority=%+v err=%v", authority, err)
	}
	if _, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: signUIChallenge(t, privateKey, challenge),
	}); !errors.Is(err, service.ErrUIChallengeInvalid) {
		t.Fatalf("replayed exchange error=%v", err)
	}
	audits := store.AuthorizationAudits()
	if len(audits) != 2 || audits[0].Action != "ui-session.exchange" || audits[1].Outcome != "authorized" {
		t.Fatalf("audits=%+v", audits)
	}
}

func TestUISessionRejectsTamperingExpiryAndUntrustedOrigins(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)}
	sessions, privateKey := newTestUISessions(t, store, clock, false)
	challenge, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: "electron-instance-2", Origin: "oc://app",
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid := ed25519.Sign(privateKey, []byte("tampered"))
	if _, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: base64.RawURLEncoding.EncodeToString(invalid),
	}); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("tampered signature error=%v", err)
	}
	if _, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: signUIChallenge(t, privateKey, challenge),
	}); !errors.Is(err, service.ErrUIChallengeInvalid) {
		t.Fatalf("consumed challenge error=%v", err)
	}
	clock.now = clock.now.Add(time.Second)
	expiring, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: "electron-instance-3", Origin: "oc://app",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(service.UIChallengeTTL)
	if _, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: expiring.Nonce, Signature: signUIChallenge(t, privateKey, expiring),
	}); !errors.Is(err, service.ErrUIChallengeExpired) && !errors.Is(err, service.ErrUIChallengeInvalid) {
		t.Fatalf("expired challenge error=%v", err)
	}
	if _, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: "electron-instance-4", Origin: "https://example.com",
	}); !errors.Is(err, service.ErrUIOriginDenied) {
		t.Fatalf("untrusted origin error=%v", err)
	}
}

func TestDevelopmentUISessionAcceptsOnlyExplicitLoopbackHTTPOrigin(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)}
	sessions, _ := newTestUISessions(t, store, clock, true)
	if _, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: "web-sidecar-1", Origin: "http://127.0.0.1:4173",
	}); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{
		"http://localhost:4173", "https://127.0.0.1:4173", "http://127.0.0.1:4173/path", "http://127.0.0.1",
	} {
		if _, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
			ClientInstance: "web-sidecar-2", Origin: origin,
		}); !errors.Is(err, service.ErrUIOriginDenied) {
			t.Fatalf("origin=%q error=%v", origin, err)
		}
	}
}

func TestUIChallengeHTTPBootstrapUnlocksCreatorProjectRoute(t *testing.T) {
	parallelAPITest(t)
	store := repository.NewMemoryProjects()
	clock := &mutableClock{now: time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)}
	sessions, privateKey := newTestUISessions(t, store, clock, false)
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), nil, nil, projects, reads, activity, runs, edits, editReads,
		media, assetReads, sourceAccess, nil, nil, nil, nil, nil, sessions,
	)
	server := httptest.NewServer(mux)
	defer server.Close()

	challengeResponse := postJSON(t, server, "/v1/auth/ui/challenges", map[string]string{
		"clientInstance": "electron-http-1", "origin": "oc://app",
	}, "")
	if challengeResponse.Code != http.StatusOK {
		t.Fatalf("challenge status=%d body=%s", challengeResponse.Code, challengeResponse.Body.String())
	}
	var challenge service.UIChallengeResult
	if err := json.NewDecoder(challengeResponse.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	sessionResponse := postJSON(t, server, "/v1/auth/ui/sessions", service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: signUIChallenge(t, privateKey, challenge),
	}, "")
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
	var session service.UISessionResult
	if err := json.NewDecoder(sessionResponse.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	create := postJSON(t, server, "/v1/projects", map[string]string{
		"requestId": "gesture:http-auth-project:1", "name": "Authorized story",
	}, session.Session)
	if create.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	activityRequest := httptest.NewRequest(http.MethodGet, server.URL+"/v1/activity?after=0", nil)
	activityRequest.Header.Set("X-Open-Cut-UI-Session", session.Session)
	activityResponse := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(activityResponse, activityRequest)
	if activityResponse.Code != http.StatusOK {
		t.Fatalf("activity status=%d body=%s", activityResponse.Code, activityResponse.Body.String())
	}
	var page application.ActivityPage
	if err := json.NewDecoder(activityResponse.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Cursor.String() != "1" || page.Cursor.String() != "1" {
		t.Fatalf("activity page=%+v", page)
	}

	streamContext, cancelStream := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelStream()
	streamRequest, err := http.NewRequestWithContext(
		streamContext, http.MethodGet, server.URL+"/v1/events?after=1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	streamRequest.Header.Set("Accept", "text/event-stream")
	streamRequest.Header.Set("X-Open-Cut-UI-Session", session.Session)
	streamResponse, err := server.Client().Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	if streamResponse.StatusCode != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", streamResponse.StatusCode, readBody(t, streamResponse))
	}
	if contentType := streamResponse.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("stream content-type=%q", contentType)
	}
	streamReader := bufio.NewReader(streamResponse.Body)
	readyFrame := ""
	for range 4 {
		line, readErr := streamReader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		readyFrame += line
	}
	if readyFrame != "retry: 1000\nevent: ready\ndata: {\"after\":\"1\"}\n\n" {
		t.Fatalf("stream ready frame=%q", readyFrame)
	}
	cancelStream()
	streamResponse.Body.Close()
	if strings.Contains(challengeResponse.Body.String()+sessionResponse.Body.String(), "private") {
		t.Fatal("bootstrap response exposed private material")
	}
}

func newTestUISessions(
	t *testing.T,
	store application.AuthorizationRepository,
	clock application.Clock,
	development bool,
) (*service.UISessionService, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := service.NewUISessionService(context.Background(), service.UISessionConfig{
		InstallationID: "installation-ui-test", InstallationGeneration: 1, CellGeneration: 7,
		PublicKey: publicKey, AllowedOrigins: []string{"oc://app"}, AllowDevelopmentOrigin: development,
	}, store, application.UUIDv7IdentityGenerator{}, clock, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return sessions, privateKey
}

func signUIChallenge(t *testing.T, key ed25519.PrivateKey, challenge service.UIChallengeResult) string {
	t.Helper()
	payload, err := base64.RawURLEncoding.DecodeString(challenge.SigningPayload)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))
}

func authorizeTestUI(
	t *testing.T,
	sessions *service.UISessionService,
	privateKey ed25519.PrivateKey,
	clientInstance string,
) application.Authority {
	t.Helper()
	challenge, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: clientInstance, Origin: "oc://app",
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: signUIChallenge(t, privateKey, challenge),
	})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := sessions.Authorize(context.Background(), service.AuthorizationRequest{
		Method: http.MethodGet, Route: "/v1/projects", UISession: issued.Session,
	})
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func issueTestUISession(
	t *testing.T,
	sessions *service.UISessionService,
	privateKey ed25519.PrivateKey,
	clientInstance string,
) string {
	t.Helper()
	challenge, err := sessions.ChallengeUI(context.Background(), service.UIChallengeRequest{
		ClientInstance: clientInstance, Origin: "oc://app",
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.ExchangeUI(context.Background(), service.UISessionRequest{
		Nonce: challenge.Nonce, Signature: signUIChallenge(t, privateKey, challenge),
	})
	if err != nil {
		t.Fatal(err)
	}
	return issued.Session
}

func postJSON(t *testing.T, server *httptest.Server, path string, body any, session string) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	if session != "" {
		request.Header.Set("X-Open-Cut-UI-Session", session)
	}
	response := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(response, request)
	return response
}
