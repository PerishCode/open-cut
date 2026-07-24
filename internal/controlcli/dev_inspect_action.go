package controlcli

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"
)

const devActionSettleExpression = `new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))`
const devActionViewportExpression = `({ width: innerWidth, height: innerHeight })`

type devRendererAction struct {
	Kind string
	Role string
	Name string
}

type devRendererActionReceipt struct {
	Kind   string     `json:"kind"`
	Role   string     `json:"role"`
	Name   string     `json:"name"`
	Input  string     `json:"input"`
	Point  [2]float64 `json:"point"`
	Bounds [4]float64 `json:"bounds"`
}

type devActionAXNode struct {
	Ignored          bool               `json:"ignored"`
	BackendDOMNodeID int64              `json:"backendDOMNodeId"`
	Role             devSnapshotAXValue `json:"role"`
	Name             devSnapshotAXValue `json:"name"`
	Properties       []struct {
		Name  string             `json:"name"`
		Value devSnapshotAXValue `json:"value"`
	} `json:"properties"`
}

func parseDevRendererAction(kind, role, name string) (*devRendererAction, error) {
	kind = strings.TrimSpace(kind)
	role = strings.TrimSpace(role)
	name = strings.TrimSpace(name)
	if kind == "" {
		if role != "" || name != "" {
			return nil, fmt.Errorf("--role and --name require --action")
		}
		return nil, nil
	}
	if kind != "click" {
		return nil, fmt.Errorf("unsupported --action %q", kind)
	}
	if role == "" || name == "" {
		return nil, fmt.Errorf("--action click requires exact --role and --name")
	}
	if len(role) > 80 || !validDevActionRole(role) {
		return nil, fmt.Errorf("--role must be a bounded accessible role token")
	}
	if len(name) > 240 || strings.ContainsFunc(name, unicode.IsControl) {
		return nil, fmt.Errorf("--name must be bounded text without control characters")
	}
	return &devRendererAction{Kind: kind, Role: role, Name: name}, nil
}

func validDevActionRole(role string) bool {
	for index, character := range role {
		if character > unicode.MaxASCII {
			return false
		}
		if index == 0 {
			if !unicode.IsLetter(character) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '-' {
			return false
		}
	}
	return true
}

func performDevRendererAction(
	ctx context.Context,
	cdp devCDPCaller,
	action devRendererAction,
) (devRendererActionReceipt, error) {
	if action.Kind != "click" {
		return devRendererActionReceipt{}, fmt.Errorf("unsupported renderer action %q", action.Kind)
	}
	var document struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := cdp.Call(ctx, "DOM.getDocument", map[string]any{"depth": 0}, &document); err != nil {
		return devRendererActionReceipt{}, err
	}
	if document.Root.NodeID == 0 {
		return devRendererActionReceipt{}, fmt.Errorf("renderer document root is unavailable")
	}
	var query struct {
		Nodes []devActionAXNode `json:"nodes"`
	}
	if err := cdp.Call(ctx, "Accessibility.queryAXTree", map[string]any{
		"nodeId": document.Root.NodeID, "accessibleName": action.Name, "role": action.Role,
	}, &query); err != nil {
		return devRendererActionReceipt{}, err
	}
	matches := make([]devActionAXNode, 0, len(query.Nodes))
	seen := make(map[int64]struct{})
	for _, node := range query.Nodes {
		if node.Ignored || node.BackendDOMNodeID == 0 ||
			snapshotValueString(node.Role.Value) != action.Role ||
			snapshotValueString(node.Name.Value) != action.Name {
			continue
		}
		if _, exists := seen[node.BackendDOMNodeID]; exists {
			continue
		}
		seen[node.BackendDOMNodeID] = struct{}{}
		matches = append(matches, node)
	}
	if len(matches) == 0 {
		return devRendererActionReceipt{}, fmt.Errorf(
			"no renderer node exactly matches role %q and name %q", action.Role, action.Name,
		)
	}
	if len(matches) > 1 {
		return devRendererActionReceipt{}, fmt.Errorf(
			"%d renderer nodes match role %q and name %q; the action is ambiguous",
			len(matches), action.Role, action.Name,
		)
	}
	target := matches[0]
	for _, property := range target.Properties {
		if property.Name == "disabled" && snapshotValueBool(property.Value.Value) {
			return devRendererActionReceipt{}, fmt.Errorf(
				"renderer node role %q and name %q is disabled", action.Role, action.Name,
			)
		}
	}
	backendNodeID := target.BackendDOMNodeID
	if err := cdp.Call(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{
		"backendNodeId": backendNodeID,
	}, nil); err != nil {
		return devRendererActionReceipt{}, err
	}
	quad, err := devActionTargetQuad(ctx, cdp, backendNodeID)
	if err != nil {
		return devRendererActionReceipt{}, err
	}
	_, bounds, err := devActionQuadGeometry(quad)
	if err != nil {
		return devRendererActionReceipt{}, err
	}
	viewport, err := devActionViewport(ctx, cdp)
	if err != nil {
		return devRendererActionReceipt{}, err
	}
	point, err := devActionVisiblePoint(bounds, viewport)
	if err != nil {
		return devRendererActionReceipt{}, err
	}
	events := []map[string]any{
		{"type": "mouseMoved", "x": point[0], "y": point[1]},
		{"type": "mousePressed", "x": point[0], "y": point[1], "button": "left", "buttons": 1, "clickCount": 1},
		{"type": "mouseReleased", "x": point[0], "y": point[1], "button": "left", "buttons": 0, "clickCount": 1},
	}
	for _, event := range events {
		if err := cdp.Call(ctx, "Input.dispatchMouseEvent", event, nil); err != nil {
			return devRendererActionReceipt{}, err
		}
	}
	var settled any
	if err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": devActionSettleExpression, "returnByValue": true, "awaitPromise": true,
	}, &settled); err != nil {
		return devRendererActionReceipt{}, err
	}
	return devRendererActionReceipt{
		Kind: action.Kind, Role: action.Role, Name: action.Name, Input: "cdp",
		Point: point, Bounds: bounds,
	}, nil
}

func devActionViewport(ctx context.Context, cdp devCDPCaller) ([2]float64, error) {
	var evaluated struct {
		Result struct {
			Value struct {
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": devActionViewportExpression, "returnByValue": true,
	}, &evaluated); err != nil {
		return [2]float64{}, err
	}
	if evaluated.Result.Value.Width <= 1 || evaluated.Result.Value.Height <= 1 {
		return [2]float64{}, fmt.Errorf("renderer viewport is unavailable")
	}
	return [2]float64{evaluated.Result.Value.Width, evaluated.Result.Value.Height}, nil
}

func devActionVisiblePoint(bounds [4]float64, viewport [2]float64) ([2]float64, error) {
	left, top := max(bounds[0], 0), max(bounds[1], 0)
	right := min(bounds[0]+bounds[2], viewport[0])
	bottom := min(bounds[1]+bounds[3], viewport[1])
	if right-left <= 1 || bottom-top <= 1 {
		return [2]float64{}, fmt.Errorf("renderer action target has no visible clickable area")
	}
	rounded := func(value float64) float64 { return math.Round(value*100) / 100 }
	return [2]float64{rounded((left + right) / 2), rounded((top + bottom) / 2)}, nil
}

func devActionTargetQuad(ctx context.Context, cdp devCDPCaller, backendNodeID int64) ([]float64, error) {
	var content struct {
		Quads [][]float64 `json:"quads"`
	}
	if err := cdp.Call(ctx, "DOM.getContentQuads", map[string]any{
		"backendNodeId": backendNodeID,
	}, &content); err == nil {
		var selected []float64
		selectedArea := 0.0
		for _, quad := range content.Quads {
			_, bounds, geometryErr := devActionQuadGeometry(quad)
			if geometryErr != nil {
				continue
			}
			area := bounds[2] * bounds[3]
			if area > selectedArea {
				selected, selectedArea = quad, area
			}
		}
		if len(selected) == 8 {
			return selected, nil
		}
	}
	var box struct {
		Model struct {
			Border []float64 `json:"border"`
		} `json:"model"`
	}
	if err := cdp.Call(ctx, "DOM.getBoxModel", map[string]any{
		"backendNodeId": backendNodeID,
	}, &box); err != nil {
		return nil, err
	}
	if len(box.Model.Border) != 8 {
		return nil, fmt.Errorf("renderer action target has no clickable box")
	}
	return box.Model.Border, nil
}

func devActionQuadGeometry(quad []float64) ([2]float64, [4]float64, error) {
	if len(quad) != 8 {
		return [2]float64{}, [4]float64{}, fmt.Errorf("renderer action target has an invalid content quad")
	}
	minX, maxX := quad[0], quad[0]
	minY, maxY := quad[1], quad[1]
	x, y := 0.0, 0.0
	for index := 0; index < len(quad); index += 2 {
		x += quad[index]
		y += quad[index+1]
		minX, maxX = min(minX, quad[index]), max(maxX, quad[index])
		minY, maxY = min(minY, quad[index+1]), max(maxY, quad[index+1])
	}
	width, height := maxX-minX, maxY-minY
	if width <= 1 || height <= 1 {
		return [2]float64{}, [4]float64{}, fmt.Errorf("renderer action target has no clickable area")
	}
	rounded := func(value float64) float64 { return math.Round(value*100) / 100 }
	return [2]float64{rounded(x / 4), rounded(y / 4)},
		[4]float64{rounded(minX), rounded(minY), rounded(width), rounded(height)}, nil
}

func snapshotValueBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return false
	}
}
