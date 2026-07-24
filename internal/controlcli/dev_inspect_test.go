package controlcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectDevInputFileRequiresNonEmptyRegularBytes(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "story.webm")
	if err := os.WriteFile(valid, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, size, err := inspectDevInputFile(valid)
	if err != nil || path != valid || size != 5 {
		t.Fatalf("path=%q size=%d err=%v", path, size, err)
	}

	empty := filepath.Join(root, "empty.webm")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectDevInputFile(empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty input error = %v", err)
	}
	if _, _, err := inspectDevInputFile(root); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory input error = %v", err)
	}
}

func TestDevInspectRequiresAnExplicitObservation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runDevInspect(context.Background(), devInspectOptions{}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--snapshot") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runDevInspect(context.Background(), devInspectOptions{
		evaluate: "1", match: "Source",
	}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--match requires --snapshot") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCaptureDevRendererSnapshotCombinesBrowserSemanticsAndLayout(t *testing.T) {
	cdp := &fakeSnapshotCDP{
		runtimeResponse: map[string]any{
			"result": map[string]any{"value": map[string]any{
				"page": map[string]any{
					"url": "oc://app/", "title": "Open Cut", "readyState": "complete", "visibilityState": "visible",
				},
				"viewport": map[string]any{
					"outerWidth": 1280, "outerHeight": 800, "innerWidth": 1280, "innerHeight": 768,
					"devicePixelRatio": 2,
				},
				"document": map[string]any{
					"clientWidth": 1280, "clientHeight": 768, "scrollWidth": 1280, "scrollHeight": 768,
					"overflowX": false, "overflowY": false,
				},
				"layout": map[string]any{
					"truncated": false,
					"nodes": []any{map[string]any{
						"tag": "section", "role": "region", "name": "Viewer", "visible": true, "clipped": false,
						"bounds": []any{300, 54, 640, 714},
					}},
				},
			}},
		},
		accessibilityResponse: map[string]any{"nodes": []any{
			map[string]any{
				"nodeId": "root", "role": map[string]any{"value": "RootWebArea"},
				"name": map[string]any{"value": "Open Cut"},
			},
			map[string]any{
				"nodeId": "generic", "parentId": "root", "role": map[string]any{"value": "generic"},
				"name": map[string]any{"value": ""},
			},
			map[string]any{
				"nodeId": "button", "parentId": "root", "role": map[string]any{"value": "button"},
				"name": map[string]any{"value": "Add footage"},
				"properties": []any{
					map[string]any{"name": "focused", "value": map[string]any{"value": true}},
					map[string]any{"name": "settable", "value": map[string]any{"value": true}},
				},
			},
			map[string]any{
				"nodeId": "ignored", "parentId": "root", "ignored": true,
				"role": map[string]any{"value": "button"}, "name": map[string]any{"value": "Hidden"},
			},
		}},
	}

	snapshot, err := captureDevRendererSnapshotWith(context.Background(), cdp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cdp.calls, ",") != "Runtime.evaluate,Accessibility.getFullAXTree" {
		t.Fatalf("calls=%v", cdp.calls)
	}
	if snapshot.Page.URL != "oc://app/" || snapshot.Viewport.InnerHeight != 768 || snapshot.Document.OverflowY {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if len(snapshot.Layout.Nodes) != 1 || snapshot.Layout.Nodes[0].Name != "Viewer" {
		t.Fatalf("layout=%+v", snapshot.Layout)
	}
	if snapshot.Summary.AccessibilityNodes != 2 || snapshot.Summary.LayoutNodes != 1 ||
		snapshot.Summary.VisibleLayoutNodes != 1 || snapshot.Summary.PageOverflow {
		t.Fatalf("summary=%+v", snapshot.Summary)
	}
	if len(snapshot.Accessibility.Nodes) != 2 {
		t.Fatalf("accessibility=%+v", snapshot.Accessibility)
	}
	button := snapshot.Accessibility.Nodes[1]
	if button.Depth != 1 || button.Role != "button" || button.Name != "Add footage" ||
		button.Properties["focused"] != true {
		t.Fatalf("button=%+v", button)
	}
	if _, leaked := button.Properties["settable"]; leaked {
		t.Fatalf("unbounded AX property leaked: %+v", button.Properties)
	}
}

func TestNormalizeDevAccessibilityIsBounded(t *testing.T) {
	nodes := make([]devSnapshotRawAXNode, maximumDevSnapshotNodes+1)
	for index := range nodes {
		nodes[index].NodeID = string(rune(index + 1))
		nodes[index].Role.Value = "button"
		nodes[index].Name.Value = "Action"
	}
	snapshot := normalizeDevAccessibility(nodes)
	if len(snapshot.Nodes) != maximumDevSnapshotNodes || !snapshot.Truncated {
		t.Fatalf("nodes=%d truncated=%v", len(snapshot.Nodes), snapshot.Truncated)
	}
}

func TestFilterDevRendererSnapshotMatchesRoleNameAndClipOwner(t *testing.T) {
	snapshot := devRendererSnapshot{
		Accessibility: devSnapshotAccessibility{Nodes: []devSnapshotAXNode{
			{Role: "button", Name: "Back to Sequence"},
			{Role: "region", Name: "Source range controls"},
		}},
		Layout: devSnapshotLayout{Nodes: []devSnapshotLayoutNode{
			{Tag: "section", Role: "region", Name: "Source preview"},
			{Tag: "button", Role: "button", Name: "Continue", ClippedBy: []string{"Source preview"}},
			{Tag: "aside", Role: "complementary", Name: "Agent"},
		}},
	}

	filtered := filterDevRendererSnapshot(snapshot, " source ")
	if filtered.Filter == nil || filtered.Filter.Match != "source" ||
		filtered.Filter.AccessibilityNodes != 1 || filtered.Filter.LayoutNodes != 2 {
		t.Fatalf("filter=%+v", filtered.Filter)
	}
	if filtered.Accessibility.Nodes[0].Name != "Source range controls" ||
		filtered.Layout.Nodes[1].Name != "Continue" {
		t.Fatalf("snapshot=%+v", filtered)
	}
}

type fakeSnapshotCDP struct {
	calls                 []string
	runtimeResponse       any
	accessibilityResponse any
}

func (fake *fakeSnapshotCDP) Call(_ context.Context, method string, parameters any, result any) error {
	fake.calls = append(fake.calls, method)
	if method == "Runtime.evaluate" {
		arguments, ok := parameters.(map[string]any)
		expression, _ := arguments["expression"].(string)
		if !ok || expression != devRendererSnapshotExpression || arguments["returnByValue"] != true {
			return &snapshotTestError{"unexpected Runtime.evaluate parameters"}
		}
		return copySnapshotTestValue(fake.runtimeResponse, result)
	}
	if method == "Accessibility.getFullAXTree" {
		return copySnapshotTestValue(fake.accessibilityResponse, result)
	}
	return &snapshotTestError{"unexpected CDP method " + method}
}

func copySnapshotTestValue(value, destination any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, destination)
}

type snapshotTestError struct {
	message string
}

func (failure *snapshotTestError) Error() string {
	return failure.message
}
