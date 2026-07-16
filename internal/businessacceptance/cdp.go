package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const maximumCDPResponseBytes = 1 << 20

type CDPClient struct {
	connection *websocket.Conn
	nextID     int64
}

type cdpTarget struct {
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func ConnectCreatorCDP(ctx context.Context, endpoint string) (*CDPClient, error) {
	base, err := url.Parse(endpoint)
	if err != nil || base.Scheme != "http" || base.Hostname() != "127.0.0.1" || base.Port() == "" ||
		(base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" || base.User != nil {
		return nil, fmt.Errorf("Creator CDP endpoint must be an explicit loopback HTTP origin")
	}
	listURL := base.ResolveReference(&url.URL{Path: "/json/list"})
	var target cdpTarget
	if err := poll(ctx, 100*time.Millisecond, func() (bool, error) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, listURL.String(), nil)
		if requestErr != nil {
			return false, requestErr
		}
		response, requestErr := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if requestErr != nil {
			return false, nil
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return false, nil
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maximumCDPResponseBytes+1))
		if readErr != nil || len(body) > maximumCDPResponseBytes {
			return false, fmt.Errorf("read Creator CDP targets: %w", readErr)
		}
		var targets []cdpTarget
		if json.Unmarshal(body, &targets) != nil {
			return false, nil
		}
		for _, candidate := range targets {
			if strings.HasPrefix(candidate.URL, "oc://app/") && candidate.WebSocketDebuggerURL != "" {
				target = candidate
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("discover installed Creator target: %w", err)
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect installed Creator target: %w", err)
	}
	return &CDPClient{connection: connection}, nil
}

func (client *CDPClient) Close() error {
	if client == nil || client.connection == nil {
		return nil
	}
	return client.connection.Close()
}

func (client *CDPClient) Call(ctx context.Context, method string, parameters any, result any) error {
	if client == nil || client.connection == nil {
		return fmt.Errorf("Creator CDP is not connected")
	}
	client.nextID++
	id := client.nextID
	if deadline, ok := ctx.Deadline(); ok {
		_ = client.connection.SetWriteDeadline(deadline)
		_ = client.connection.SetReadDeadline(deadline)
	}
	if err := client.connection.WriteJSON(map[string]any{"id": id, "method": method, "params": parameters}); err != nil {
		return err
	}
	for {
		_, body, err := client.connection.ReadMessage()
		if err != nil {
			return err
		}
		if len(body) > maximumCDPResponseBytes {
			return fmt.Errorf("Creator CDP response exceeded its limit")
		}
		var response cdpResponse
		if json.Unmarshal(body, &response) != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("Creator CDP %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
		}
		if result != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return err
			}
		}
		return nil
	}
}

func poll(ctx context.Context, interval time.Duration, operation func() (bool, error)) error {
	for {
		ready, err := operation()
		if err != nil || ready {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
