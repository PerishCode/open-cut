package broker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/internal/atomicfile"
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/auth"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/gofrs/flock"
	"github.com/gorilla/websocket"
)

var ErrAlreadyRunning = errors.New("cell broker is already running")

var delegatedSubjectPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)

const maximumSessionTokenTTL = 7 * 24 * time.Hour

type Options struct {
	Identity         cell.Identity
	Paths            layout.CellPaths
	Generation       uint64
	HeartbeatTimeout time.Duration
	OwnerTokenTTL    time.Duration
	UpdateTransition func(context.Context, protocol.UpdateTransitionRequest) (protocol.UpdateTransitionResponse, error)
}

type session struct {
	status     protocol.SessionStatus
	conn       *websocket.Conn
	subscribed bool
	write      sync.Mutex
}

type Broker struct {
	identity         cell.Identity
	paths            layout.CellPaths
	descriptor       protocol.ControlDescriptor
	manager          *auth.Manager
	lock             *flock.Flock
	listener         net.Listener
	server           *http.Server
	heartbeatTimeout time.Duration

	mu         sync.RWMutex
	sessions   map[string]*session
	revision   uint64
	changed    chan struct{}
	registered chan string
	ready      chan string
	closed     chan struct{}
	close      sync.Once
	ownerMu    sync.Mutex
	updateMu   sync.Mutex
	update     func(context.Context, protocol.UpdateTransitionRequest) (protocol.UpdateTransitionResponse, error)
}

func Start(options Options) (*Broker, error) {
	if err := options.Identity.Validate(); err != nil {
		return nil, err
	}
	if options.HeartbeatTimeout <= 0 {
		options.HeartbeatTimeout = 15 * time.Second
	}
	if options.OwnerTokenTTL <= 0 {
		options.OwnerTokenTTL = maximumSessionTokenTTL
	}
	if err := options.Paths.Ensure(); err != nil {
		return nil, err
	}
	cellLock := flock.New(options.Paths.BrokerLock)
	locked, err := cellLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire broker lock: %w", err)
	}
	if !locked {
		return nil, ErrAlreadyRunning
	}
	fail := func(err error) (*Broker, error) {
		cellLock.Unlock()
		return nil, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fail(fmt.Errorf("listen on loopback TCP: %w", err))
	}
	sessionID, err := randomID(18)
	if err != nil {
		listener.Close()
		return fail(err)
	}
	manager, err := auth.NewManager(sessionID, options.Generation)
	if err != nil {
		listener.Close()
		return fail(err)
	}
	descriptor := protocol.ControlDescriptor{
		Schema: 1, Protocol: protocol.Version, Address: listener.Addr().String(), PID: os.Getpid(),
		SessionID: sessionID, Generation: options.Generation, StartedAt: time.Now().UTC(),
	}
	ownerToken, err := manager.Mint("oc-control", protocol.RoleOwner, []protocol.Capability{
		protocol.CapabilityObserve, protocol.CapabilityLifecycle, protocol.CapabilityUpdateTransition,
	}, options.OwnerTokenTTL)
	if err != nil {
		listener.Close()
		return fail(err)
	}
	if err := atomicfile.WriteJSON(options.Paths.ControlFile, descriptor, 0o600); err != nil {
		listener.Close()
		return fail(err)
	}
	if err := atomicfile.Write(options.Paths.OwnerTokenFile, []byte(ownerToken+"\n"), 0o600); err != nil {
		listener.Close()
		os.Remove(options.Paths.ControlFile)
		return fail(err)
	}

	broker := &Broker{
		identity: options.Identity, paths: options.Paths, descriptor: descriptor,
		manager: manager, lock: cellLock, listener: listener,
		heartbeatTimeout: options.HeartbeatTimeout,
		update:           options.UpdateTransition,
		sessions:         make(map[string]*session),
		changed:          make(chan struct{}, 1),
		registered:       make(chan string, 32),
		ready:            make(chan string, 32),
		closed:           make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", broker.handleHealth)
	mux.HandleFunc("GET /v1/status", broker.handleStatus)
	mux.HandleFunc("POST /v1/control", broker.handleControl)
	mux.HandleFunc("POST /v1/capabilities/sidecar", broker.handleDelegateSidecar)
	mux.HandleFunc("POST /v1/capabilities/renew", broker.handleRenewCapability)
	mux.HandleFunc("POST /v1/update/transition", broker.handleUpdateTransition)
	mux.HandleFunc("GET /v1/sessions/register", broker.handleRegister)
	broker.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := broker.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			broker.Close()
		}
	}()
	go broker.broadcastStatusChanges()
	go broker.renewOwnerCapability(options.OwnerTokenTTL)
	return broker, nil
}

func (broker *Broker) Descriptor() protocol.ControlDescriptor { return broker.descriptor }

func (broker *Broker) MintSidecarToken(subject string, ttl time.Duration) (string, error) {
	return broker.manager.Mint(subject, protocol.RoleSidecar, []protocol.Capability{
		protocol.CapabilityRuntimeReady, protocol.CapabilityLifecycle, protocol.CapabilityObserve,
	}, ttl)
}

func (broker *Broker) MintRuntimeToken(subject string, ttl time.Duration) (string, error) {
	return broker.manager.Mint(subject, protocol.RoleRuntime, []protocol.Capability{
		protocol.CapabilityRuntimeReady, protocol.CapabilityLifecycle, protocol.CapabilityObserve,
		protocol.CapabilityDelegateSidecar, protocol.CapabilityUpdateTransition,
	}, ttl)
}

func (broker *Broker) WaitReady(ctx context.Context, subject string) error {
	for {
		broker.mu.RLock()
		registered := broker.sessions[subject]
		isReady := registered != nil && registered.status.Ready
		broker.mu.RUnlock()
		if isReady {
			return nil
		}
		select {
		case readySubject := <-broker.ready:
			if readySubject == subject {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-broker.closed:
			return fmt.Errorf("broker closed before %s became ready", subject)
		}
	}
}

func (broker *Broker) WaitRegistered(ctx context.Context, subject string) error {
	for {
		broker.mu.RLock()
		isRegistered := broker.sessions[subject] != nil
		broker.mu.RUnlock()
		if isRegistered {
			return nil
		}
		select {
		case registeredSubject := <-broker.registered:
			if registeredSubject == subject {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-broker.closed:
			return fmt.Errorf("broker closed before %s registered", subject)
		}
	}
}

func (broker *Broker) WaitAbsent(ctx context.Context, subject string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		broker.mu.RLock()
		registered := broker.sessions[subject] != nil
		broker.mu.RUnlock()
		if !registered {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		case <-broker.closed:
			return fmt.Errorf("broker closed while waiting for %s to exit", subject)
		}
	}
}

func (broker *Broker) Close() error {
	var result error
	broker.close.Do(func() {
		close(broker.closed)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if broker.server != nil {
			result = broker.server.Shutdown(ctx)
		}
		broker.mu.Lock()
		for _, registered := range broker.sessions {
			registered.conn.Close()
		}
		broker.sessions = make(map[string]*session)
		broker.mu.Unlock()
		os.Remove(broker.paths.ControlFile)
		broker.ownerMu.Lock()
		os.Remove(broker.paths.OwnerTokenFile)
		broker.ownerMu.Unlock()
		if err := broker.lock.Unlock(); result == nil && err != nil {
			result = err
		}
	})
	return result
}

func (broker *Broker) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, protocol.Health{
		Schema: 1, Protocol: protocol.Version,
		SessionID: broker.descriptor.SessionID, Generation: broker.descriptor.Generation,
	})
}

func (broker *Broker) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if _, ok := broker.authorize(writer, request, protocol.CapabilityObserve); !ok {
		return
	}
	writeJSON(writer, http.StatusOK, broker.status())
}

func (broker *Broker) handleControl(writer http.ResponseWriter, request *http.Request) {
	if _, ok := broker.authorize(writer, request, protocol.CapabilityLifecycle); !ok {
		return
	}
	var control protocol.ControlRequest
	if err := json.NewDecoder(request.Body).Decode(&control); err != nil || (control.Command != "show" && control.Command != "shutdown") {
		writeError(writer, http.StatusBadRequest, "command must be show or shutdown")
		return
	}
	accepted := broker.broadcast(protocol.ServerEvent{Type: "command", Command: control.Command})
	writeJSON(writer, http.StatusAccepted, protocol.ControlResponse{Accepted: accepted})
}

func (broker *Broker) handleDelegateSidecar(writer http.ResponseWriter, request *http.Request) {
	claims, ok := broker.authorize(writer, request, protocol.CapabilityDelegateSidecar)
	if !ok {
		return
	}
	if claims.Role != protocol.RoleRuntime {
		writeError(writer, http.StatusForbidden, "only the runtime role may delegate sidecars")
		return
	}
	var delegation protocol.DelegateRequest
	if err := json.NewDecoder(request.Body).Decode(&delegation); err != nil || !delegatedSubjectPattern.MatchString(delegation.Subject) || delegation.Subject == claims.Subject || delegation.Subject == "oc-control" {
		writeError(writer, http.StatusBadRequest, "a valid non-reserved sidecar subject is required")
		return
	}
	ttl := time.Duration(delegation.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	remaining := time.Until(time.Unix(claims.ExpiresAt, 0))
	if ttl > maximumSessionTokenTTL {
		ttl = maximumSessionTokenTTL
	}
	if ttl > remaining {
		ttl = remaining
	}
	if ttl <= 0 {
		writeError(writer, http.StatusUnauthorized, "runtime capability has expired")
		return
	}
	capabilities := []protocol.Capability{
		protocol.CapabilityRuntimeReady, protocol.CapabilityLifecycle, protocol.CapabilityObserve,
	}
	seen := map[protocol.Capability]bool{
		protocol.CapabilityRuntimeReady: true, protocol.CapabilityLifecycle: true, protocol.CapabilityObserve: true,
	}
	for _, capability := range delegation.Capabilities {
		if capability != protocol.CapabilityUpdateTransition || seen[capability] {
			writeError(writer, http.StatusBadRequest, "unsupported or duplicate delegated capability")
			return
		}
		seen[capability] = true
		capabilities = append(capabilities, capability)
	}
	token, err := broker.manager.MintDelegated(delegation.Subject, protocol.RoleSidecar, capabilities, ttl, claims.Subject)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "could not mint sidecar capability")
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.DelegateResponse{
		Subject: delegation.Subject, Token: token, ExpiresAt: time.Now().UTC().Add(ttl),
	})
}

func (broker *Broker) handleRenewCapability(writer http.ResponseWriter, request *http.Request) {
	claims, ok := broker.authorize(writer, request, protocol.CapabilityRuntimeReady)
	if !ok {
		return
	}
	if claims.Role != protocol.RoleRuntime && claims.Role != protocol.RoleSidecar {
		writeError(writer, http.StatusForbidden, "only active runtime and sidecar sessions may renew")
		return
	}
	broker.mu.RLock()
	active := broker.sessions[claims.Subject] != nil
	broker.mu.RUnlock()
	if !active {
		writeError(writer, http.StatusConflict, "capability subject is not registered")
		return
	}
	var renewal protocol.RenewRequest
	if request.Body != nil {
		if err := json.NewDecoder(request.Body).Decode(&renewal); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid renewal request")
			return
		}
	}
	ttl := time.Duration(renewal.TTLSeconds) * time.Second
	if ttl <= 0 || ttl > maximumSessionTokenTTL {
		ttl = maximumSessionTokenTTL
	}
	token, err := broker.manager.MintDelegated(claims.Subject, claims.Role, claims.Capabilities, ttl, claims.DelegatedBy)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "could not renew capability")
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.RenewResponse{
		Subject: claims.Subject, Token: token, ExpiresAt: time.Now().UTC().Add(ttl),
	})
}

func (broker *Broker) handleUpdateTransition(writer http.ResponseWriter, request *http.Request) {
	_, ok := broker.authorize(writer, request, protocol.CapabilityUpdateTransition)
	if !ok {
		return
	}
	if broker.update == nil {
		writeError(writer, http.StatusForbidden, "update transitions are unavailable")
		return
	}
	var transition protocol.UpdateTransitionRequest
	if err := json.NewDecoder(request.Body).Decode(&transition); err != nil || transition.Action != "prepare-latest" {
		writeError(writer, http.StatusBadRequest, "action must be prepare-latest")
		return
	}
	broker.updateMu.Lock()
	response, err := broker.update(request.Context(), transition)
	broker.updateMu.Unlock()
	if err != nil {
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (broker *Broker) handleRegister(writer http.ResponseWriter, request *http.Request) {
	claims, ok := broker.authorize(writer, request, protocol.CapabilityRuntimeReady)
	if !ok {
		return
	}
	connection, err := websocket.Upgrade(writer, request, nil, 4096, 4096)
	if err != nil {
		return
	}
	connection.SetReadLimit(64 * 1024)
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var registration protocol.ClientEvent
	if err := connection.ReadJSON(&registration); err != nil || !broker.validRegistration(claims, registration) {
		connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid registration"), time.Now().Add(time.Second))
		connection.Close()
		return
	}
	connection.SetReadDeadline(time.Time{})
	now := time.Now().UTC()
	registered := &session{
		conn: connection,
		status: protocol.SessionStatus{
			Subject: claims.Subject, App: registration.App, InstanceID: registration.InstanceID,
			Mode: registration.Mode, Source: registration.Source,
			ConnectedAt: now, LastHeartbeat: now,
		},
	}
	broker.mu.Lock()
	if existing := broker.sessions[claims.Subject]; existing != nil {
		broker.mu.Unlock()
		connection.Close()
		return
	}
	broker.sessions[claims.Subject] = registered
	broker.revision++
	broker.mu.Unlock()
	broker.signalChange()
	select {
	case broker.registered <- claims.Subject:
	default:
	}
	err = registered.send(protocol.ServerEvent{Type: "registered"})
	if err == nil {
		broker.mu.Lock()
		registered.subscribed = true
		broker.mu.Unlock()
		broker.signalChange()
		err = broker.readSession(claims.Subject, registered)
	}
	broker.mu.Lock()
	if broker.sessions[claims.Subject] == registered {
		delete(broker.sessions, claims.Subject)
		broker.revision++
	}
	broker.mu.Unlock()
	broker.signalChange()
	connection.Close()
}

func (broker *Broker) readSession(subject string, registered *session) error {
	for {
		registered.conn.SetReadDeadline(time.Now().Add(broker.heartbeatTimeout))
		var event protocol.ClientEvent
		if err := registered.conn.ReadJSON(&event); err != nil {
			return err
		}
		now := time.Now().UTC()
		changed := false
		broker.mu.Lock()
		switch event.Type {
		case "heartbeat":
			registered.status.LastHeartbeat = now
		case "endpoint":
			if event.Name == "" || event.URL == "" {
				broker.mu.Unlock()
				return fmt.Errorf("invalid endpoint event")
			}
			registered.status.LastHeartbeat = now
			registered.status.Endpoints, changed = upsertEndpoint(
				registered.status.Endpoints,
				protocol.Endpoint{Name: event.Name, URL: event.URL},
			)
		case "ready":
			registered.status.LastHeartbeat = now
			changed = !registered.status.Ready
			registered.status.Ready = true
		case "state":
			if event.Ready == nil {
				broker.mu.Unlock()
				return fmt.Errorf("state event requires ready")
			}
			registered.status.LastHeartbeat = now
			changed = registered.status.Ready != *event.Ready
			registered.status.Ready = *event.Ready
			if !*event.Ready && len(registered.status.Endpoints) > 0 {
				registered.status.Endpoints = nil
				changed = true
			}
		case "exiting":
			broker.mu.Unlock()
			return nil
		default:
			broker.mu.Unlock()
			return fmt.Errorf("unknown sidecar event %q", event.Type)
		}
		if changed {
			broker.revision++
		}
		ready := changed && registered.status.Ready
		broker.mu.Unlock()
		if ready {
			select {
			case broker.ready <- subject:
			default:
			}
		}
		if changed {
			broker.signalChange()
		}
	}
}

func (broker *Broker) validRegistration(claims auth.Claims, event protocol.ClientEvent) bool {
	appMatches := event.App == claims.Subject || claims.Role == protocol.RoleRuntime
	return event.Type == "register" && event.App != "" && event.InstanceID != "" && appMatches && event.Mode != "" && event.Source != "" &&
		event.Channel == broker.identity.Channel && event.Namespace == broker.identity.Namespace &&
		event.SessionID == broker.descriptor.SessionID && event.Generation == broker.descriptor.Generation
}

func (broker *Broker) authorize(writer http.ResponseWriter, request *http.Request, capability protocol.Capability) (auth.Claims, bool) {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeError(writer, http.StatusUnauthorized, "bearer capability required")
		return auth.Claims{}, false
	}
	claims, err := broker.manager.Verify(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), capability)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "invalid capability")
		return auth.Claims{}, false
	}
	return claims, true
}

func (broker *Broker) status() protocol.Status {
	broker.mu.RLock()
	defer broker.mu.RUnlock()
	return broker.statusLocked()
}

func (broker *Broker) statusLocked() protocol.Status {
	sessions := make([]protocol.SessionStatus, 0, len(broker.sessions))
	for _, registered := range broker.sessions {
		copy := registered.status
		copy.Endpoints = append([]protocol.Endpoint(nil), copy.Endpoints...)
		sessions = append(sessions, copy)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Subject < sessions[j].Subject })
	return protocol.Status{
		Schema: 1, Revision: broker.revision, Channel: broker.identity.Channel, Namespace: broker.identity.Namespace,
		SessionID: broker.descriptor.SessionID, Generation: broker.descriptor.Generation, Sessions: sessions,
	}
}

func (broker *Broker) signalChange() {
	select {
	case broker.changed <- struct{}{}:
	default:
	}
}

func (broker *Broker) broadcastStatusChanges() {
	for {
		select {
		case <-broker.closed:
			return
		case <-broker.changed:
			broker.broadcastStatus()
		}
	}
}

func (broker *Broker) renewOwnerCapability(ttl time.Duration) {
	interval := ttl / 2
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-broker.closed:
			return
		case <-ticker.C:
			broker.ownerMu.Lock()
			select {
			case <-broker.closed:
				broker.ownerMu.Unlock()
				return
			default:
			}
			token, err := broker.manager.Mint("oc-control", protocol.RoleOwner, []protocol.Capability{
				protocol.CapabilityObserve, protocol.CapabilityLifecycle, protocol.CapabilityUpdateTransition,
			}, ttl)
			if err == nil {
				_ = atomicfile.Write(broker.paths.OwnerTokenFile, []byte(token+"\n"), 0o600)
			}
			broker.ownerMu.Unlock()
		}
	}
}

func (broker *Broker) broadcastStatus() {
	broker.mu.RLock()
	status := broker.statusLocked()
	sessions := make([]*session, 0, len(broker.sessions))
	for _, registered := range broker.sessions {
		if registered.subscribed {
			sessions = append(sessions, registered)
		}
	}
	broker.mu.RUnlock()
	event := protocol.ServerEvent{Type: "status", Status: &status}
	for _, registered := range sessions {
		_ = registered.send(event)
	}
}

func (broker *Broker) broadcast(event protocol.ServerEvent) int {
	broker.mu.RLock()
	sessions := make([]*session, 0, len(broker.sessions))
	for _, registered := range broker.sessions {
		sessions = append(sessions, registered)
	}
	broker.mu.RUnlock()
	accepted := 0
	for _, registered := range sessions {
		err := registered.send(event)
		if err == nil {
			accepted++
		}
	}
	return accepted
}

func (registered *session) send(event protocol.ServerEvent) error {
	registered.write.Lock()
	defer registered.write.Unlock()
	_ = registered.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	err := registered.conn.WriteJSON(event)
	_ = registered.conn.SetWriteDeadline(time.Time{})
	return err
}

func upsertEndpoint(endpoints []protocol.Endpoint, endpoint protocol.Endpoint) ([]protocol.Endpoint, bool) {
	for index := range endpoints {
		if endpoints[index].Name == endpoint.Name {
			if endpoints[index] == endpoint {
				return endpoints, false
			}
			endpoints[index] = endpoint
			return endpoints, true
		}
	}
	return append(endpoints, endpoint), true
}

func randomID(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
