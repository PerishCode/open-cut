package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/gorilla/websocket"
)

type Client struct {
	descriptor protocol.ControlDescriptor
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
	if err := client.request(ctx, http.MethodGet, "/v1/status", nil, &status); err != nil {
		return protocol.Status{}, err
	}
	return status, nil
}

func (client *Client) Control(ctx context.Context, command string) (protocol.ControlResponse, error) {
	var response protocol.ControlResponse
	if err := client.request(ctx, http.MethodPost, "/v1/control", protocol.ControlRequest{Command: command}, &response); err != nil {
		return protocol.ControlResponse{}, err
	}
	return response, nil
}

func (client *Client) DelegateSidecar(ctx context.Context, subject string, ttl time.Duration) (protocol.DelegateResponse, error) {
	var response protocol.DelegateResponse
	request := protocol.DelegateRequest{Subject: subject, TTLSeconds: int64(ttl / time.Second)}
	if err := client.request(ctx, http.MethodPost, "/v1/capabilities/sidecar", request, &response); err != nil {
		return protocol.DelegateResponse{}, err
	}
	return response, nil
}

func (client *Client) PrepareLatest(ctx context.Context) (protocol.UpdateTransitionResponse, error) {
	var response protocol.UpdateTransitionResponse
	request := protocol.UpdateTransitionRequest{Action: "prepare-latest"}
	if err := client.request(ctx, http.MethodPost, "/v1/update/transition", request, &response); err != nil {
		return protocol.UpdateTransitionResponse{}, err
	}
	return response, nil
}

func (client *Client) request(ctx context.Context, method, requestPath string, body, output any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://"+client.descriptor.Address+requestPath, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
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
	Channel   string
	Namespace string
	App       string
	Mode      string
	Source    string
}

type Session struct {
	connection *websocket.Conn
	write      sync.Mutex
}

func DialSession(ctx context.Context, descriptor protocol.ControlDescriptor, token string, registration Registration) (*Session, error) {
	endpoint := url.URL{Scheme: "ws", Host: descriptor.Address, Path: "/v1/sessions/register"}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	connection, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), headers)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("register sidecar: %s", response.Status)
		}
		return nil, err
	}
	session := &Session{connection: connection}
	if err := session.Send(protocol.ClientEvent{
		Type: "register", Channel: registration.Channel, Namespace: registration.Namespace,
		SessionID: descriptor.SessionID, Generation: descriptor.Generation,
		App: registration.App, Mode: registration.Mode, Source: registration.Source,
	}); err != nil {
		connection.Close()
		return nil, err
	}
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ack protocol.ServerEvent
	if err := connection.ReadJSON(&ack); err != nil || ack.Type != "registered" {
		connection.Close()
		return nil, fmt.Errorf("broker did not acknowledge registration")
	}
	connection.SetReadDeadline(time.Time{})
	return session, nil
}

func (session *Session) Heartbeat() error {
	return session.Send(protocol.ClientEvent{Type: "heartbeat"})
}
func (session *Session) Ready() error { return session.Send(protocol.ClientEvent{Type: "ready"}) }
func (session *Session) Endpoint(name, value string) error {
	return session.Send(protocol.ClientEvent{Type: "endpoint", Name: name, URL: value})
}

func (session *Session) Send(event protocol.ClientEvent) error {
	session.write.Lock()
	defer session.write.Unlock()
	return session.connection.WriteJSON(event)
}

func (session *Session) ReadCommand(ctx context.Context) (string, error) {
	if deadline, ok := ctx.Deadline(); ok {
		session.connection.SetReadDeadline(deadline)
		defer session.connection.SetReadDeadline(time.Time{})
	}
	var event protocol.ServerEvent
	if err := session.connection.ReadJSON(&event); err != nil {
		return "", err
	}
	if event.Type != "command" {
		return "", fmt.Errorf("unexpected server event %q", event.Type)
	}
	return event.Command, nil
}

func (session *Session) Close(code int) error {
	_ = session.Send(protocol.ClientEvent{Type: "exiting", Code: code})
	return session.connection.Close()
}
