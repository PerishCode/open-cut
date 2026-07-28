package controlcli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
)

func TestNormalizeDevNetworkEventsJoinsRequestsAndRelaysEventSourceFrames(t *testing.T) {
	event := func(method, params string) businessacceptance.CDPEvent {
		return businessacceptance.CDPEvent{Method: method, Params: json.RawMessage(params)}
	}
	events := []businessacceptance.CDPEvent{
		event("Network.requestWillBeSent",
			`{"requestId":"r1","request":{"method":"POST","url":"http://127.0.0.1:9/v1/things/preview"}}`),
		event("Network.responseReceived",
			`{"requestId":"r1","response":{"status":422,"mimeType":"application/problem+json"}}`),
		event("Network.requestWillBeSent",
			`{"requestId":"r2","request":{"method":"GET","url":"http://127.0.0.1:9/v1/activity/stream"}}`),
		event("Network.responseReceived",
			`{"requestId":"r2","response":{"status":200,"mimeType":"text/event-stream"}}`),
		event("Network.eventSourceMessageReceived",
			`{"requestId":"r2","eventName":"activity","data":"{\"cursor\":\"41\"}"}`),
		event("Network.requestWillBeSent",
			`{"requestId":"r3","request":{"method":"GET","url":"http://127.0.0.1:9/unrelated.css"}}`),
		event("Network.loadingFailed",
			`{"requestId":"r3","errorText":"net::ERR_ABORTED"}`),
	}
	report := normalizeDevNetworkEvents(events, 2, devNetworkObservation{
		Duration: time.Second, Match: "/v1/",
	})
	if report.EventCount != len(events) || report.DroppedEvents != 2 || report.Truncated {
		t.Fatalf("unexpected report envelope: %+v", report)
	}
	if len(report.Requests) != 2 ||
		report.Requests[0].Method != "POST" || report.Requests[0].Status != 422 ||
		report.Requests[0].MimeType != "application/problem+json" ||
		report.Requests[1].MimeType != "text/event-stream" {
		t.Fatalf("unexpected requests: %+v", report.Requests)
	}
	if len(report.EventSourceMessages) != 1 ||
		report.EventSourceMessages[0].EventName != "activity" ||
		report.EventSourceMessages[0].URL != "http://127.0.0.1:9/v1/activity/stream" ||
		report.EventSourceMessages[0].Data != `{"cursor":"41"}` {
		t.Fatalf("unexpected event source messages: %+v", report.EventSourceMessages)
	}
}

func TestNormalizeDevNetworkEventsFiltersUnmatchedActivity(t *testing.T) {
	events := []businessacceptance.CDPEvent{
		{Method: "Network.requestWillBeSent", Params: json.RawMessage(
			`{"requestId":"r1","request":{"method":"GET","url":"http://127.0.0.1:9/asset.png"}}`)},
		{Method: "Network.eventSourceMessageReceived", Params: json.RawMessage(
			`{"requestId":"r1","eventName":"noise","data":"x"}`)},
	}
	report := normalizeDevNetworkEvents(events, 0, devNetworkObservation{Duration: time.Second, Match: "/v1/"})
	if len(report.Requests) != 0 || len(report.EventSourceMessages) != 0 {
		t.Fatalf("expected everything filtered, got %+v", report)
	}
}
