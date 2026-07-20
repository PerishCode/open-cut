package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/gorilla/websocket"
)

type Client struct {
	descriptor protocol.ControlDescriptor
	tokenMu    sync.RWMutex
	token      string
	http       *http.Client
}

func Load(controlFile, tokenFile string) (*Client, error) {
	descriptorBytes, err := os.ReadFile(controlFile)
	if err != nil {
		return nil, fmt.Errorf("read control descriptor: %w", err)
	}
	var descriptor protocol.ControlDescriptor
	if err := json.Unmarshal(descriptorBytes, &descriptor); err != nil {
		return nil, fmt.Errorf("decode control descriptor: %w", err)
	}
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read capability token: %w", err)
	}
	return New(descriptor, strings.TrimSpace(string(tokenBytes))), nil
}

func New(descriptor protocol.ControlDescriptor, token string) *Client {
	return &Client{descriptor: descriptor, token: token, http: &http.Client{Timeout: 5 * time.Second}}
}

func (client *Client) Status(ctx context.Context) (protocol.Status, error) {
	var status protocol.Status
	if err := client.request(ctx, protocol.SchemeStatus, protocol.MethodStatus, protocol.RouteStatus, nil, &status); err != nil {
		return protocol.Status{}, err
	}
	return status, nil
}

func (client *Client) Control(ctx context.Context, command protocol.ControlCommand) (protocol.ControlResponse, error) {
	var response protocol.ControlResponse
	if err := client.request(ctx, protocol.SchemeBroadcastControl, protocol.MethodBroadcastControl, protocol.RouteBroadcastControl, protocol.ControlRequest{Command: command}, &response); err != nil {
		return protocol.ControlResponse{}, err
	}
	return response, nil
}

func (client *Client) DelegateSidecar(
	ctx context.Context,
	subject string,
	ttl time.Duration,
	capabilities []protocol.Capability,
) (protocol.DelegateResponse, error) {
	var response protocol.DelegateResponse
	request := protocol.DelegateRequest{
		Subject: subject, TTLSeconds: int64(ttl / time.Second),
		Capabilities: append([]protocol.Capability(nil), capabilities...),
	}
	if err := client.request(ctx, protocol.SchemeDelegateSidecarCapability, protocol.MethodDelegateSidecarCapability, protocol.RouteDelegateSidecarCapability, request, &response); err != nil {
		return protocol.DelegateResponse{}, err
	}
	return response, nil
}

func (client *Client) PrepareLatest(ctx context.Context) (protocol.UpdateTransitionResponse, error) {
	var response protocol.UpdateTransitionResponse
	request := protocol.UpdateTransitionRequest{Action: protocol.UpdateActionPrepareLatest}
	if err := client.request(ctx, protocol.SchemePrepareLatestUpdate, protocol.MethodPrepareLatestUpdate, protocol.RoutePrepareLatestUpdate, request, &response); err != nil {
		return protocol.UpdateTransitionResponse{}, err
	}
	return response, nil
}

func (client *Client) Renew(ctx context.Context, ttl time.Duration) (protocol.RenewResponse, error) {
	var response protocol.RenewResponse
	request := protocol.RenewRequest{TTLSeconds: int64(ttl / time.Second)}
	if err := client.request(ctx, protocol.SchemeRenewCapability, protocol.MethodRenewCapability, protocol.RouteRenewCapability, request, &response); err != nil {
		return protocol.RenewResponse{}, err
	}
	client.setToken(response.Token)
	return response, nil
}

func (client *Client) setToken(token string) {
	client.tokenMu.Lock()
	client.token = token
	client.tokenMu.Unlock()
}

func (client *Client) request(ctx context.Context, scheme, method, requestPath string, body, output any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, scheme+"://"+client.descriptor.Address+requestPath, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	client.tokenMu.RLock()
	token := client.token
	client.tokenMu.RUnlock()
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("control request returned %s", response.Status)
	}
	return json.NewDecoder(response.Body).Decode(output)
}

type Registration struct {
	Channel    string
	Namespace  string
	App        string
	InstanceID string
	Mode       protocol.LifecycleMode
	Source     string
}

// developmentAbandonWindow bounds how long a dev or harness peer keeps
// reconnecting to a lost control broker before failing closed. Production and
// packaged peers keep the unbounded retry: their runner owns cell lifetime.
// Variable only so tests can compress the window.
var developmentAbandonWindow = 60 * time.Second

type Session struct {
	descriptor   protocol.ControlDescriptor
	token        string
	registration Registration

	mu            sync.Mutex
	connection    *websocket.Conn
	endpoints     map[string]string
	desiredReady  bool
	events        chan protocol.ServerEvent
	closed        chan struct{}
	closeOnce     sync.Once
	abandoned     chan struct{}
	abandonOnce   sync.Once
	abandonWindow time.Duration
}

func DialSession(ctx context.Context, descriptor protocol.ControlDescriptor, token string, registration Registration) (*Session, error) {
	if registration.InstanceID == "" {
		instanceID, err := randomInstanceID()
		if err != nil {
			return nil, err
		}
		registration.InstanceID = instanceID
	}
	connection, err := dialConnection(ctx, descriptor, token, registration)
	if err != nil {
		return nil, err
	}
	session := &Session{
		descriptor: descriptor, token: token, registration: registration,
		connection: connection, endpoints: make(map[string]string),
		events: make(chan protocol.ServerEvent, 64), closed: make(chan struct{}),
		abandoned: make(chan struct{}), abandonWindow: abandonWindow(registration.Mode),
	}
	go session.run(connection)
	return session, nil
}

// abandonWindow selects the fail-closed reconnect bound for a session mode; a
// zero window keeps the unbounded retry.
func abandonWindow(mode protocol.LifecycleMode) time.Duration {
	if mode == protocol.LifecycleModeDev || mode == protocol.LifecycleModeHarness {
		return developmentAbandonWindow
	}
	return 0
}

// Abandoned is closed when the session gave up reconnecting to a lost broker
// and failed closed; the owning process must treat this as a shutdown signal.
func (session *Session) Abandoned() <-chan struct{} {
	return session.abandoned
}

func (session *Session) abandon() {
	session.closeOnce.Do(func() {
		close(session.closed)
	})
	session.abandonOnce.Do(func() {
		close(session.abandoned)
	})
}

func (session *Session) Heartbeat() error {
	return session.send(protocol.ClientEvent{Type: protocol.EventHeartbeat})
}
func (session *Session) Ready() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.desiredReady = true
	return session.replayLocked()
}
func (session *Session) NotReady() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.desiredReady = false
	ready := false
	return session.sendLocked(protocol.ClientEvent{Type: protocol.EventState, Ready: &ready})
}
func (session *Session) Endpoint(name, value string) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.endpoints[name] = value
	if !session.desiredReady {
		return nil
	}
	return session.sendLocked(protocol.ClientEvent{Type: protocol.EventEndpoint, Name: name, URL: value})
}

func (session *Session) send(event protocol.ClientEvent) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.sendLocked(event)
}

func (session *Session) ReadCommand(ctx context.Context) (protocol.ControlCommand, error) {
	for {
		event, err := session.ReadEvent(ctx)
		if err != nil {
			return "", err
		}
		if event.Type == protocol.EventCommand {
			return event.Command, nil
		}
	}
}

func (session *Session) ReadEvent(ctx context.Context) (protocol.ServerEvent, error) {
	select {
	case event := <-session.events:
		return event, nil
	case <-ctx.Done():
		return protocol.ServerEvent{}, ctx.Err()
	case <-session.closed:
		return protocol.ServerEvent{}, fmt.Errorf("sidecar session is closed")
	}
}

func (session *Session) Close(code int) error {
	var result error
	session.closeOnce.Do(func() {
		close(session.closed)
		session.mu.Lock()
		if session.connection != nil {
			_ = session.sendLocked(protocol.ClientEvent{Type: protocol.EventExiting, Code: code})
			result = session.connection.Close()
			session.connection = nil
		}
		session.mu.Unlock()
	})
	return result
}

func (session *Session) Renew(ctx context.Context, ttl time.Duration) (protocol.RenewResponse, error) {
	session.mu.Lock()
	token := session.token
	session.mu.Unlock()
	control := New(session.descriptor, token)
	response, err := control.Renew(ctx, ttl)
	if err != nil {
		return protocol.RenewResponse{}, err
	}
	session.mu.Lock()
	session.token = response.Token
	session.mu.Unlock()
	return response, nil
}

func (session *Session) run(connection *websocket.Conn) {
	current := connection
	for {
		var event protocol.ServerEvent
		err := current.ReadJSON(&event)
		if err == nil {
			if event.Type != protocol.EventCommand && event.Type != protocol.EventStatus {
				continue
			}
			if event.Type == protocol.EventStatus {
				select {
				case session.events <- event:
				default:
				}
				continue
			}
			select {
			case session.events <- event:
			case <-session.closed:
				return
			}
			continue
		}

		session.mu.Lock()
		if session.connection == current {
			session.connection = nil
		}
		session.mu.Unlock()
		_ = current.Close()
		select {
		case <-session.closed:
			return
		default:
		}

		lostAt := time.Now()
		delay := 50 * time.Millisecond
		for {
			reconnectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			session.mu.Lock()
			token := session.token
			session.mu.Unlock()
			next, reconnectErr := dialConnection(reconnectContext, session.descriptor, token, session.registration)
			cancel()
			if reconnectErr == nil {
				session.mu.Lock()
				select {
				case <-session.closed:
					session.mu.Unlock()
					next.Close()
					return
				default:
				}
				session.connection = next
				reconnectErr = session.replayLocked()
				session.mu.Unlock()
				if reconnectErr == nil {
					current = next
					break
				}
				next.Close()
			}
			if session.abandonWindow > 0 && time.Since(lostAt) >= session.abandonWindow {
				session.abandon()
				return
			}
			select {
			case <-session.closed:
				return
			case <-time.After(delay):
			}
			delay = min(delay*2, 2*time.Second)
		}
	}
}

func (session *Session) replayLocked() error {
	if !session.desiredReady {
		ready := false
		return session.sendLocked(protocol.ClientEvent{Type: protocol.EventState, Ready: &ready})
	}
	names := make([]string, 0, len(session.endpoints))
	for name := range session.endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := session.sendLocked(protocol.ClientEvent{Type: protocol.EventEndpoint, Name: name, URL: session.endpoints[name]}); err != nil {
			return err
		}
	}
	ready := true
	return session.sendLocked(protocol.ClientEvent{Type: protocol.EventState, Ready: &ready})
}

func (session *Session) sendLocked(event protocol.ClientEvent) error {
	if session.connection == nil {
		return fmt.Errorf("sidecar session is reconnecting")
	}
	_ = session.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := session.connection.WriteJSON(event)
	_ = session.connection.SetWriteDeadline(time.Time{})
	return err
}

func dialConnection(
	ctx context.Context,
	descriptor protocol.ControlDescriptor,
	token string,
	registration Registration,
) (*websocket.Conn, error) {
	endpoint := url.URL{Scheme: protocol.SchemeRegisterSession, Host: descriptor.Address, Path: protocol.RouteRegisterSession}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	connection, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), headers)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("register sidecar: %s", response.Status)
		}
		return nil, err
	}
	if err := connection.WriteJSON(protocol.ClientEvent{
		Type: protocol.EventRegister, Channel: registration.Channel, Namespace: registration.Namespace,
		SessionID: descriptor.SessionID, Generation: descriptor.Generation,
		App: registration.App, InstanceID: registration.InstanceID,
		Mode: registration.Mode, Source: registration.Source,
	}); err != nil {
		connection.Close()
		return nil, err
	}
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var acknowledgement protocol.ServerEvent
	if err := connection.ReadJSON(&acknowledgement); err != nil || acknowledgement.Type != protocol.EventRegistered {
		connection.Close()
		return nil, fmt.Errorf("broker did not acknowledge registration")
	}
	connection.SetReadDeadline(time.Time{})
	return connection, nil
}

func randomInstanceID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
