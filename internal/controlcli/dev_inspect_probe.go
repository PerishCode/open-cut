package controlcli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minimumDevViewportEdge        = 320
	maximumDevViewportEdge        = 10_000
	maximumDevExceptionTextLength = 300
)

type devEvaluation struct {
	valueType string
	value     any
	exception string
}

func (evaluation devEvaluation) syntaxError() bool {
	return strings.Contains(evaluation.exception, "SyntaxError")
}

// evaluateDevExpression runs one Runtime.evaluate and reports the value or a
// bounded exception description instead of an opaque failure.
func evaluateDevExpression(ctx context.Context, cdp devCDPCaller, expression string) (devEvaluation, error) {
	var evaluated struct {
		Result struct {
			Type  string `json:"type"`
			Value any    `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expression, "returnByValue": true, "awaitPromise": true,
	}, &evaluated); err != nil {
		return devEvaluation{}, err
	}
	if evaluated.Exception != nil {
		description := strings.TrimSpace(evaluated.Exception.Exception.Description)
		if description == "" {
			description = strings.TrimSpace(evaluated.Exception.Text)
		}
		if description == "" {
			description = "unknown exception"
		}
		if firstLine, _, split := strings.Cut(description, "\n"); split {
			description = firstLine
		}
		if len(description) > maximumDevExceptionTextLength {
			description = description[:maximumDevExceptionTextLength] + "…"
		}
		return devEvaluation{exception: description}, nil
	}
	return devEvaluation{valueType: evaluated.Result.Type, value: evaluated.Result.Value}, nil
}

func parseDevViewport(value string) (int, int, error) {
	widthText, heightText, found := strings.Cut(strings.TrimSpace(value), "x")
	if !found {
		return 0, 0, fmt.Errorf("--viewport must be WxH, e.g. 1440x900")
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(widthText))
	height, heightErr := strconv.Atoi(strings.TrimSpace(heightText))
	if widthErr != nil || heightErr != nil ||
		width < minimumDevViewportEdge || width > maximumDevViewportEdge ||
		height < minimumDevViewportEdge || height > maximumDevViewportEdge {
		return 0, 0, fmt.Errorf(
			"--viewport edges must be integers within [%d, %d]", minimumDevViewportEdge, maximumDevViewportEdge,
		)
	}
	return width, height, nil
}

// applyDevViewport resizes the renderer's outer window, waits for the settle
// delay, and verifies the platform honored the request.
func applyDevViewport(
	ctx context.Context,
	cdp devCDPCaller,
	width, height int,
	settle time.Duration,
) ([2]int, error) {
	resize := fmt.Sprintf("window.resizeTo(%d, %d)", width, height)
	if evaluation, err := evaluateDevExpression(ctx, cdp, resize); err != nil {
		return [2]int{}, err
	} else if evaluation.exception != "" {
		return [2]int{}, fmt.Errorf("renderer rejected the resize: %s", evaluation.exception)
	}
	if settle > 0 {
		timer := time.NewTimer(settle)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return [2]int{}, ctx.Err()
		}
	}
	evaluation, err := evaluateDevExpression(ctx, cdp, "({ width: outerWidth, height: outerHeight })")
	if err != nil {
		return [2]int{}, err
	}
	outer, ok := evaluation.value.(map[string]any)
	outerWidth, widthOK := devViewportEdge(outer["width"])
	outerHeight, heightOK := devViewportEdge(outer["height"])
	if !ok || !widthOK || !heightOK {
		return [2]int{}, fmt.Errorf("renderer window size is unavailable after resizing")
	}
	if outerWidth != width || outerHeight != height {
		return [2]int{}, fmt.Errorf(
			"renderer window is %dx%d after requesting %dx%d; native fullscreen or the platform may pin the size",
			outerWidth, outerHeight, width, height,
		)
	}
	return [2]int{outerWidth, outerHeight}, nil
}

func devViewportEdge(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok {
		return 0, false
	}
	return int(number), true
}

// devRegionClip resolves a CSS selector to a screenshot clip in viewport
// coordinates after revealing the node.
func devRegionClip(ctx context.Context, cdp devCDPCaller, selector string) (map[string]any, error) {
	var document struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := cdp.Call(ctx, "DOM.getDocument", map[string]any{"depth": 1}, &document); err != nil {
		return nil, err
	}
	var query struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := cdp.Call(ctx, "DOM.querySelector", map[string]any{
		"nodeId": document.Root.NodeID, "selector": selector,
	}, &query); err != nil {
		return nil, err
	}
	if query.NodeID == 0 {
		return nil, fmt.Errorf("no renderer node matches selector %q", selector)
	}
	if err := cdp.Call(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"nodeId": query.NodeID}, nil); err != nil {
		return nil, err
	}
	var box struct {
		Model struct {
			Border []float64 `json:"border"`
		} `json:"model"`
	}
	if err := cdp.Call(ctx, "DOM.getBoxModel", map[string]any{"nodeId": query.NodeID}, &box); err != nil {
		return nil, err
	}
	_, bounds, err := devActionQuadGeometry(box.Model.Border)
	if err != nil {
		return nil, fmt.Errorf("selector %q has no visible box", selector)
	}
	return map[string]any{
		"x": bounds[0], "y": bounds[1], "width": bounds[2], "height": bounds[3], "scale": 1,
	}, nil
}
