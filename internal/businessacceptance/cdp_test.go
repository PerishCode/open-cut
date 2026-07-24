package businessacceptance

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCDPEventQueueIsBoundedAndDrainedAtomically(t *testing.T) {
	client := &CDPClient{}
	for index := 0; index < maximumPendingCDPEvents+2; index++ {
		client.queueEvent(CDPEvent{
			Method: "Runtime.consoleAPICalled",
			Params: json.RawMessage(fmt.Sprintf(`{"index":%d}`, index)),
		})
	}
	events, dropped := client.DrainEvents()
	if len(events) != maximumPendingCDPEvents || dropped != 2 ||
		string(events[0].Params) != `{"index":2}` ||
		string(events[len(events)-1].Params) != fmt.Sprintf(`{"index":%d}`, maximumPendingCDPEvents+1) {
		t.Fatalf("events=%d dropped=%d first=%s last=%s", len(events), dropped, events[0].Params, events[len(events)-1].Params)
	}
	events, dropped = client.DrainEvents()
	if len(events) != 0 || dropped != 0 {
		t.Fatalf("second drain events=%d dropped=%d", len(events), dropped)
	}
}

func TestCDPEventQueueCopiesCallerBytesAndIgnoresEmptyMethods(t *testing.T) {
	client := &CDPClient{}
	params := json.RawMessage(`{"message":"original"}`)
	client.queueEvent(CDPEvent{Method: "Log.entryAdded", Params: params})
	copy(params, `{"message":"changed!"}`)
	client.queueEvent(CDPEvent{Params: json.RawMessage(`{"ignored":true}`)})

	events, _ := client.DrainEvents()
	if len(events) != 1 || string(events[0].Params) != `{"message":"original"}` {
		t.Fatalf("events=%+v", events)
	}
}
