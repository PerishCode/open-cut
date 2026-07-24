package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
)

const (
	minimumDevErrorObservation = 50 * time.Millisecond
	maximumDevErrorObservation = 30 * time.Second
	maximumDevRendererErrors   = 100
)

type devErrorObservation struct {
	Started  time.Time
	Duration time.Duration
}

type devRendererErrorReport struct {
	DurationMilliseconds int64              `json:"durationMilliseconds"`
	EventCount           int                `json:"eventCount"`
	DroppedEvents        int                `json:"droppedEvents"`
	Count                int                `json:"count"`
	Truncated            bool               `json:"truncated"`
	Items                []devRendererError `json:"items"`
}

type devRendererError struct {
	Source       string `json:"source"`
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	LineNumber   int64  `json:"lineNumber,omitempty"`
	ColumnNumber int64  `json:"columnNumber,omitempty"`
}

type devErrorCDP interface {
	devCDPCaller
	DrainEvents() ([]businessacceptance.CDPEvent, int)
}

func startDevErrorObservation(
	ctx context.Context,
	cdp devErrorCDP,
	duration time.Duration,
) (*devErrorObservation, error) {
	if err := cdp.Call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		return nil, err
	}
	if err := cdp.Call(ctx, "Log.enable", map[string]any{}, nil); err != nil {
		return nil, err
	}
	// Runtime.enable may replay console entries retained by the renderer from
	// before this inspect session. Establish a clean boundary only after both
	// domains are active so the report describes this observation window.
	cdp.DrainEvents()
	return &devErrorObservation{Started: time.Now(), Duration: duration}, nil
}

func finishDevErrorObservation(
	ctx context.Context,
	cdp devErrorCDP,
	observation devErrorObservation,
) (devRendererErrorReport, error) {
	if remaining := observation.Duration - time.Since(observation.Started); remaining > 0 {
		waitMilliseconds := max(remaining.Milliseconds(), 1)
		expression := fmt.Sprintf(
			`new Promise((resolve) => setTimeout(resolve, %d))`,
			waitMilliseconds,
		)
		if err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
			"expression": expression, "returnByValue": true, "awaitPromise": true,
		}, nil); err != nil {
			return devRendererErrorReport{}, err
		}
	}
	events, dropped := cdp.DrainEvents()
	return normalizeDevRendererErrors(events, dropped, observation.Duration), nil
}

func normalizeDevRendererErrors(
	events []businessacceptance.CDPEvent,
	dropped int,
	duration time.Duration,
) devRendererErrorReport {
	report := devRendererErrorReport{
		DurationMilliseconds: duration.Milliseconds(),
		EventCount:           len(events),
		DroppedEvents:        dropped,
		Items:                make([]devRendererError, 0),
	}
	seen := make(map[string]struct{})
	for _, event := range events {
		item, relevant := devRendererErrorFromEvent(event)
		if !relevant {
			continue
		}
		item.Text = boundDevErrorText(item.Text)
		item.URL = boundDevErrorText(item.URL)
		key := item.Source + "\x00" + item.Text + "\x00" + item.URL + "\x00" +
			strconv.FormatInt(item.LineNumber, 10) + "\x00" + strconv.FormatInt(item.ColumnNumber, 10)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if len(report.Items) == maximumDevRendererErrors {
			report.Truncated = true
			continue
		}
		report.Items = append(report.Items, item)
	}
	report.Count = len(report.Items)
	return report
}

func devRendererErrorFromEvent(event businessacceptance.CDPEvent) (devRendererError, bool) {
	switch event.Method {
	case "Runtime.exceptionThrown":
		var payload struct {
			ExceptionDetails struct {
				Text         string `json:"text"`
				URL          string `json:"url"`
				LineNumber   int64  `json:"lineNumber"`
				ColumnNumber int64  `json:"columnNumber"`
				Exception    struct {
					Description string `json:"description"`
					Value       any    `json:"value"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(event.Params, &payload) != nil {
			return devRendererError{}, false
		}
		details := payload.ExceptionDetails
		text := details.Exception.Description
		if text == "" {
			text = snapshotValueString(details.Exception.Value)
		}
		if text == "" {
			text = details.Text
		}
		return devRendererError{
			Source: "exception", Text: text, URL: details.URL,
			LineNumber: details.LineNumber, ColumnNumber: details.ColumnNumber,
		}, true
	case "Runtime.consoleAPICalled":
		var payload struct {
			Type string `json:"type"`
			Args []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
				Type        string `json:"type"`
			} `json:"args"`
			StackTrace struct {
				CallFrames []struct {
					URL          string `json:"url"`
					LineNumber   int64  `json:"lineNumber"`
					ColumnNumber int64  `json:"columnNumber"`
				} `json:"callFrames"`
			} `json:"stackTrace"`
		}
		if json.Unmarshal(event.Params, &payload) != nil ||
			(payload.Type != "error" && payload.Type != "assert") {
			return devRendererError{}, false
		}
		parts := make([]string, 0, len(payload.Args))
		for _, argument := range payload.Args {
			text := snapshotValueString(argument.Value)
			if text == "" {
				text = argument.Description
			}
			if text == "" {
				text = argument.Type
			}
			parts = append(parts, text)
		}
		item := devRendererError{Source: "console", Text: strings.Join(parts, " ")}
		if len(payload.StackTrace.CallFrames) > 0 {
			frame := payload.StackTrace.CallFrames[0]
			item.URL, item.LineNumber, item.ColumnNumber = frame.URL, frame.LineNumber, frame.ColumnNumber
		}
		return item, true
	case "Log.entryAdded":
		var payload struct {
			Entry struct {
				Level        string `json:"level"`
				Text         string `json:"text"`
				URL          string `json:"url"`
				LineNumber   int64  `json:"lineNumber"`
				ColumnNumber int64  `json:"columnNumber"`
			} `json:"entry"`
		}
		if json.Unmarshal(event.Params, &payload) != nil || payload.Entry.Level != "error" {
			return devRendererError{}, false
		}
		return devRendererError{
			Source: "log", Text: payload.Entry.Text, URL: payload.Entry.URL,
			LineNumber: payload.Entry.LineNumber, ColumnNumber: payload.Entry.ColumnNumber,
		}, true
	default:
		return devRendererError{}, false
	}
}

func boundDevErrorText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 500 {
		value = string(runes[:500])
	}
	return value
}
