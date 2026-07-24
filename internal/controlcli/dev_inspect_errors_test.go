package controlcli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
)

func TestDevErrorObservationEnablesDomainsWaitsAndDrains(t *testing.T) {
	cdp := &fakeErrorCDP{
		events: []businessacceptance.CDPEvent{
			{Method: "Runtime.consoleAPICalled", Params: json.RawMessage(`{"type":"error","args":[{"value":"stale"}]}`)},
		},
		dropped: 2,
		waitEvents: []businessacceptance.CDPEvent{
			{Method: "Runtime.consoleAPICalled", Params: json.RawMessage(`{"type":"warning"}`)},
		},
		waitDropped: 1,
	}
	observation, err := startDevErrorObservation(context.Background(), cdp, 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	report, err := finishDevErrorObservation(context.Background(), cdp, *observation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cdp.methods, ",") != "Runtime.enable,Log.enable,Runtime.evaluate" {
		t.Fatalf("methods=%v", cdp.methods)
	}
	wait := cdp.parameters[2]
	expression, _ := wait["expression"].(string)
	if !strings.Contains(expression, "setTimeout") || wait["awaitPromise"] != true {
		t.Fatalf("wait=%v", wait)
	}
	if report.DurationMilliseconds != 250 || report.EventCount != 1 || report.DroppedEvents != 1 ||
		report.Count != 0 || report.Items == nil {
		t.Fatalf("report=%+v", report)
	}
}

func TestNormalizeDevRendererErrorsKeepsOnlyBoundedErrors(t *testing.T) {
	console := businessacceptance.CDPEvent{
		Method: "Runtime.consoleAPICalled",
		Params: json.RawMessage(`{
			"type":"error",
			"args":[{"type":"string","value":"Failed"},{"type":"object","description":"Error: bad"}],
			"stackTrace":{"callFrames":[{"url":"oc://app/index.js","lineNumber":12,"columnNumber":4}]}
		}`),
	}
	report := normalizeDevRendererErrors([]businessacceptance.CDPEvent{
		{
			Method: "Runtime.exceptionThrown",
			Params: json.RawMessage(`{
				"exceptionDetails":{
					"text":"Uncaught","url":"oc://app/index.js","lineNumber":7,"columnNumber":2,
					"exception":{"description":"Error: boom"}
				}
			}`),
		},
		console,
		console,
		{
			Method: "Log.entryAdded",
			Params: json.RawMessage(`{
				"entry":{"level":"error","text":"Network failed","url":"oc://app/index.js","lineNumber":20}
			}`),
		},
		{Method: "Runtime.consoleAPICalled", Params: json.RawMessage(`{"type":"warning","args":[]}`)},
		{Method: "Runtime.executionContextCreated", Params: json.RawMessage(`{}`)},
	}, 3, 500*time.Millisecond)

	if report.DurationMilliseconds != 500 || report.EventCount != 6 || report.DroppedEvents != 3 ||
		report.Count != 3 || report.Truncated {
		t.Fatalf("report=%+v", report)
	}
	if report.Items[0].Source != "exception" || report.Items[0].Text != "Error: boom" ||
		report.Items[1].Source != "console" || report.Items[1].Text != "Failed Error: bad" ||
		report.Items[2].Source != "log" || report.Items[2].Text != "Network failed" {
		t.Fatalf("items=%+v", report.Items)
	}
}

func TestBoundDevErrorTextNormalizesWhitespaceAndRunes(t *testing.T) {
	value := "  first\n\t" + strings.Repeat("界", 600)
	bounded := boundDevErrorText(value)
	if strings.ContainsAny(bounded, "\n\t") || len([]rune(bounded)) != 500 ||
		!strings.HasPrefix(bounded, "first 界") {
		t.Fatalf("bounded=%q runes=%d", bounded, len([]rune(bounded)))
	}
}

type fakeErrorCDP struct {
	methods     []string
	parameters  []map[string]any
	events      []businessacceptance.CDPEvent
	dropped     int
	waitEvents  []businessacceptance.CDPEvent
	waitDropped int
}

func (fake *fakeErrorCDP) Call(_ context.Context, method string, parameters any, _ any) error {
	arguments, _ := parameters.(map[string]any)
	fake.methods = append(fake.methods, method)
	fake.parameters = append(fake.parameters, arguments)
	if method == "Runtime.evaluate" {
		fake.events = append(fake.events, fake.waitEvents...)
		fake.dropped += fake.waitDropped
	}
	return nil
}

func (fake *fakeErrorCDP) DrainEvents() ([]businessacceptance.CDPEvent, int) {
	events, dropped := fake.events, fake.dropped
	fake.events = nil
	fake.dropped = 0
	return events, dropped
}
