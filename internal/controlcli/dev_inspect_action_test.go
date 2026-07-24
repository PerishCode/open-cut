package controlcli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseDevRendererActionFailsClosed(t *testing.T) {
	for _, test := range []struct {
		kind, role, name string
		valid            bool
	}{
		{valid: true},
		{kind: "click", role: "tab", name: "Streams", valid: true},
		{role: "tab", name: "Streams"},
		{kind: "press", role: "tab", name: "Streams"},
		{kind: "click", role: "tab"},
		{kind: "click", role: "not a role", name: "Streams"},
		{kind: "click", role: "tab", name: "Streams\nNow"},
	} {
		action, err := parseDevRendererAction(test.kind, test.role, test.name)
		if test.valid && err != nil {
			t.Fatalf("kind=%q role=%q name=%q err=%v", test.kind, test.role, test.name, err)
		}
		if !test.valid && err == nil {
			t.Fatalf("kind=%q role=%q name=%q action=%+v", test.kind, test.role, test.name, action)
		}
	}
}

func TestPerformDevRendererClickUsesUniqueAXTargetAndTrustedInput(t *testing.T) {
	cdp := &fakeActionCDP{
		nodes: []any{actionAXNode(42, "tab", "Streams", false)},
		quads: [][]float64{{10, 20, 110, 20, 110, 60, 10, 60}},
	}
	receipt, err := performDevRendererAction(context.Background(), cdp, devRendererAction{
		Kind: "click", Role: "tab", Name: "Streams",
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedCalls := strings.Join([]string{
		"DOM.getDocument", "Accessibility.queryAXTree", "DOM.scrollIntoViewIfNeeded", "DOM.getContentQuads",
		"Runtime.evaluate",
		"Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Runtime.evaluate",
	}, ",")
	if strings.Join(cdp.methods(), ",") != expectedCalls {
		t.Fatalf("calls=%v", cdp.methods())
	}
	if receipt.Kind != "click" || receipt.Role != "tab" || receipt.Name != "Streams" || receipt.Input != "cdp" ||
		receipt.Point != [2]float64{60, 40} || receipt.Bounds != [4]float64{10, 20, 100, 40} {
		t.Fatalf("receipt=%+v", receipt)
	}
	query := cdp.calls[1].parameters
	if query["nodeId"] != int64(7) || query["accessibleName"] != "Streams" || query["role"] != "tab" {
		t.Fatalf("query=%v", query)
	}
	viewport := cdp.calls[4].parameters
	moved := cdp.calls[5].parameters
	pressed := cdp.calls[6].parameters
	released := cdp.calls[7].parameters
	if viewport["expression"] != devActionViewportExpression || moved["type"] != "mouseMoved" ||
		pressed["type"] != "mousePressed" || released["type"] != "mouseReleased" {
		t.Fatalf("input=%v", cdp.calls[4:8])
	}
	if pressed["button"] != "left" || pressed["buttons"] != 1 || pressed["clickCount"] != 1 {
		t.Fatalf("pressed=%v", pressed)
	}
	settle := cdp.calls[8].parameters
	if settle["expression"] != devActionSettleExpression || settle["awaitPromise"] != true {
		t.Fatalf("settle=%v", settle)
	}
}

func TestPerformDevRendererClickRejectsAmbiguousAndDisabledTargets(t *testing.T) {
	ambiguous := &fakeActionCDP{nodes: []any{
		actionAXNode(42, "button", "Clear", false),
		actionAXNode(43, "button", "Clear", false),
	}}
	_, err := performDevRendererAction(context.Background(), ambiguous, devRendererAction{
		Kind: "click", Role: "button", Name: "Clear",
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || len(ambiguous.calls) != 2 {
		t.Fatalf("calls=%v err=%v", ambiguous.methods(), err)
	}

	disabled := &fakeActionCDP{nodes: []any{actionAXNode(42, "button", "Continue", true)}}
	_, err = performDevRendererAction(context.Background(), disabled, devRendererAction{
		Kind: "click", Role: "button", Name: "Continue",
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") || len(disabled.calls) != 2 {
		t.Fatalf("calls=%v err=%v", disabled.methods(), err)
	}
}

func TestPerformDevRendererClickFallsBackToStableBoxModel(t *testing.T) {
	cdp := &fakeActionCDP{
		nodes:      []any{actionAXNode(42, "button", "Projects", false)},
		contentErr: errors.New("unsupported"),
		border:     []float64{2, 4, 42, 4, 42, 24, 2, 24},
	}
	receipt, err := performDevRendererAction(context.Background(), cdp, devRendererAction{
		Kind: "click", Role: "button", Name: "Projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Point != [2]float64{22, 14} || cdp.methods()[4] != "DOM.getBoxModel" {
		t.Fatalf("receipt=%+v calls=%v", receipt, cdp.methods())
	}
}

func TestPerformDevRendererClickRejectsOffscreenTargetAfterScroll(t *testing.T) {
	cdp := &fakeActionCDP{
		nodes: []any{actionAXNode(42, "button", "Projects", false)},
		quads: [][]float64{{10, 900, 110, 900, 110, 940, 10, 940}},
	}
	_, err := performDevRendererAction(context.Background(), cdp, devRendererAction{
		Kind: "click", Role: "button", Name: "Projects",
	})
	if err == nil || !strings.Contains(err.Error(), "no visible clickable area") ||
		cdp.methods()[len(cdp.methods())-1] != "Runtime.evaluate" {
		t.Fatalf("calls=%v err=%v", cdp.methods(), err)
	}
}

func actionAXNode(backendNodeID int64, role, name string, disabled bool) map[string]any {
	node := map[string]any{
		"backendDOMNodeId": backendNodeID,
		"role":             map[string]any{"value": role},
		"name":             map[string]any{"value": name},
	}
	if disabled {
		node["properties"] = []any{
			map[string]any{"name": "disabled", "value": map[string]any{"value": true}},
		}
	}
	return node
}

type fakeActionCall struct {
	method     string
	parameters map[string]any
}

type fakeActionCDP struct {
	calls      []fakeActionCall
	nodes      []any
	quads      [][]float64
	contentErr error
	border     []float64
}

func (fake *fakeActionCDP) Call(_ context.Context, method string, parameters any, result any) error {
	arguments, _ := parameters.(map[string]any)
	fake.calls = append(fake.calls, fakeActionCall{method: method, parameters: arguments})
	switch method {
	case "DOM.getDocument":
		return copySnapshotTestValue(map[string]any{
			"root": map[string]any{"nodeId": 7},
		}, result)
	case "Accessibility.queryAXTree":
		return copySnapshotTestValue(map[string]any{"nodes": fake.nodes}, result)
	case "DOM.scrollIntoViewIfNeeded", "Input.dispatchMouseEvent":
		return nil
	case "DOM.getContentQuads":
		if fake.contentErr != nil {
			return fake.contentErr
		}
		return copySnapshotTestValue(map[string]any{"quads": fake.quads}, result)
	case "DOM.getBoxModel":
		return copySnapshotTestValue(map[string]any{
			"model": map[string]any{"border": fake.border},
		}, result)
	case "Runtime.evaluate":
		if arguments["expression"] == devActionViewportExpression {
			return copySnapshotTestValue(map[string]any{
				"result": map[string]any{"value": map[string]any{"width": 120, "height": 80}},
			}, result)
		}
		return copySnapshotTestValue(map[string]any{"result": map[string]any{"type": "undefined"}}, result)
	default:
		return errors.New("unexpected CDP method " + method)
	}
}

func (fake *fakeActionCDP) methods() []string {
	methods := make([]string, 0, len(fake.calls))
	for _, call := range fake.calls {
		methods = append(methods, call.method)
	}
	return methods
}
