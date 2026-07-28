package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
)

const maximumDevNetworkItems = 200

// devNetworkObservation is a bounded window over the live renderer's network
// activity, expressed purely in CDP terms: requests, their responses or
// failures, and EventSource message frames. The tool relays wire facts and
// interprets nothing.
type devNetworkObservation struct {
	Started  time.Time
	Duration time.Duration
	Match    string
}

type devNetworkReport struct {
	DurationMilliseconds int64                   `json:"durationMilliseconds"`
	EventCount           int                     `json:"eventCount"`
	DroppedEvents        int                     `json:"droppedEvents"`
	Truncated            bool                    `json:"truncated"`
	Requests             []devNetworkRequest     `json:"requests"`
	EventSourceMessages  []devEventSourceMessage `json:"eventSourceMessages"`
}

type devNetworkRequest struct {
	Method   string `json:"method"`
	URL      string `json:"url"`
	Status   int    `json:"status,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Failure  string `json:"failure,omitempty"`
}

type devEventSourceMessage struct {
	URL       string `json:"url,omitempty"`
	EventName string `json:"eventName"`
	Data      string `json:"data"`
}

func startDevNetworkObservation(
	ctx context.Context,
	cdp devErrorCDP,
	duration time.Duration,
	match string,
) (*devNetworkObservation, error) {
	if err := cdp.Call(ctx, "Network.enable", map[string]any{}, nil); err != nil {
		return nil, err
	}
	cdp.DrainEvents()
	return &devNetworkObservation{Started: time.Now(), Duration: duration, Match: match}, nil
}

func finishDevNetworkObservation(
	ctx context.Context,
	cdp devErrorCDP,
	observation devNetworkObservation,
) (devNetworkReport, error) {
	if remaining := observation.Duration - time.Since(observation.Started); remaining > 0 {
		waitMilliseconds := max(remaining.Milliseconds(), 1)
		expression := fmt.Sprintf(`new Promise((resolve) => setTimeout(resolve, %d))`, waitMilliseconds)
		if err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
			"expression": expression, "returnByValue": true, "awaitPromise": true,
		}, nil); err != nil {
			return devNetworkReport{}, err
		}
	}
	events, dropped := cdp.DrainEvents()
	return normalizeDevNetworkEvents(events, dropped, observation), nil
}

func normalizeDevNetworkEvents(
	events []businessacceptance.CDPEvent,
	dropped int,
	observation devNetworkObservation,
) devNetworkReport {
	report := devNetworkReport{
		DurationMilliseconds: observation.Duration.Milliseconds(),
		EventCount:           len(events),
		DroppedEvents:        dropped,
		Requests:             make([]devNetworkRequest, 0),
		EventSourceMessages:  make([]devEventSourceMessage, 0),
	}
	type pending struct {
		request devNetworkRequest
		matched bool
	}
	requests := map[string]*pending{}
	order := make([]string, 0, len(events))
	appendBounded := func(add func()) {
		if len(report.Requests)+len(report.EventSourceMessages) >= maximumDevNetworkItems {
			report.Truncated = true
			return
		}
		add()
	}
	matches := func(url string) bool {
		return observation.Match == "" || strings.Contains(url, observation.Match)
	}
	for _, event := range events {
		switch event.Method {
		case "Network.requestWillBeSent":
			var payload struct {
				RequestID string `json:"requestId"`
				Request   struct {
					Method string `json:"method"`
					URL    string `json:"url"`
				} `json:"request"`
			}
			if json.Unmarshal(event.Params, &payload) != nil || payload.RequestID == "" {
				continue
			}
			if _, exists := requests[payload.RequestID]; !exists {
				order = append(order, payload.RequestID)
			}
			requests[payload.RequestID] = &pending{
				request: devNetworkRequest{
					Method: payload.Request.Method,
					URL:    boundDevErrorText(payload.Request.URL),
				},
				matched: matches(payload.Request.URL),
			}
		case "Network.responseReceived":
			var payload struct {
				RequestID string `json:"requestId"`
				Response  struct {
					Status   int    `json:"status"`
					MimeType string `json:"mimeType"`
				} `json:"response"`
			}
			if json.Unmarshal(event.Params, &payload) != nil {
				continue
			}
			if entry, exists := requests[payload.RequestID]; exists {
				entry.request.Status = payload.Response.Status
				entry.request.MimeType = payload.Response.MimeType
			}
		case "Network.loadingFailed":
			var payload struct {
				RequestID string `json:"requestId"`
				ErrorText string `json:"errorText"`
			}
			if json.Unmarshal(event.Params, &payload) != nil {
				continue
			}
			if entry, exists := requests[payload.RequestID]; exists {
				entry.request.Failure = boundDevErrorText(payload.ErrorText)
			}
		case "Network.eventSourceMessageReceived":
			var payload struct {
				RequestID string `json:"requestId"`
				EventName string `json:"eventName"`
				Data      string `json:"data"`
			}
			if json.Unmarshal(event.Params, &payload) != nil {
				continue
			}
			url := ""
			relevant := observation.Match == ""
			if entry, exists := requests[payload.RequestID]; exists {
				url = entry.request.URL
				relevant = entry.matched
			}
			if !relevant {
				continue
			}
			appendBounded(func() {
				report.EventSourceMessages = append(report.EventSourceMessages, devEventSourceMessage{
					URL: url, EventName: payload.EventName, Data: boundDevErrorText(payload.Data),
				})
			})
		}
	}
	for _, requestID := range order {
		entry := requests[requestID]
		if entry == nil || !entry.matched {
			continue
		}
		appendBounded(func() {
			report.Requests = append(report.Requests, entry.request)
		})
	}
	return report
}
