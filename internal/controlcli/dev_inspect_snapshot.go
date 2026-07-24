package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
)

const maximumDevSnapshotNodes = 500

type devRendererSnapshot struct {
	Page          devSnapshotPage          `json:"page"`
	Viewport      devSnapshotViewport      `json:"viewport"`
	Document      devSnapshotDocument      `json:"document"`
	Summary       devSnapshotSummary       `json:"summary"`
	Filter        *devSnapshotFilter       `json:"filter,omitempty"`
	Focus         *devSnapshotLayoutNode   `json:"focus,omitempty"`
	Accessibility devSnapshotAccessibility `json:"accessibility"`
	Layout        devSnapshotLayout        `json:"layout"`
}

type devSnapshotFilter struct {
	Match              string `json:"match"`
	AccessibilityNodes int    `json:"accessibilityNodes"`
	LayoutNodes        int    `json:"layoutNodes"`
}

type devSnapshotPage struct {
	URL             string `json:"url"`
	Title           string `json:"title"`
	ReadyState      string `json:"readyState"`
	VisibilityState string `json:"visibilityState"`
}

type devSnapshotViewport struct {
	OuterWidth       float64 `json:"outerWidth"`
	OuterHeight      float64 `json:"outerHeight"`
	InnerWidth       float64 `json:"innerWidth"`
	InnerHeight      float64 `json:"innerHeight"`
	DevicePixelRatio float64 `json:"devicePixelRatio"`
}

type devSnapshotDocument struct {
	ClientWidth  float64 `json:"clientWidth"`
	ClientHeight float64 `json:"clientHeight"`
	ScrollWidth  float64 `json:"scrollWidth"`
	ScrollHeight float64 `json:"scrollHeight"`
	OverflowX    bool    `json:"overflowX"`
	OverflowY    bool    `json:"overflowY"`
}

type devSnapshotAccessibility struct {
	Nodes     []devSnapshotAXNode `json:"nodes"`
	Truncated bool                `json:"truncated"`
}

type devSnapshotSummary struct {
	AccessibilityNodes int  `json:"accessibilityNodes"`
	LayoutNodes        int  `json:"layoutNodes"`
	VisibleLayoutNodes int  `json:"visibleLayoutNodes"`
	ClippedLayoutNodes int  `json:"clippedLayoutNodes"`
	DisabledControls   int  `json:"disabledControls"`
	PageOverflow       bool `json:"pageOverflow"`
}

type devSnapshotAXNode struct {
	Depth       int            `json:"depth"`
	Role        string         `json:"role"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

type devSnapshotLayout struct {
	Nodes     []devSnapshotLayoutNode `json:"nodes"`
	Truncated bool                    `json:"truncated"`
}

type devSnapshotLayoutNode struct {
	Tag       string             `json:"tag"`
	Role      string             `json:"role,omitempty"`
	Name      string             `json:"name,omitempty"`
	Hidden    bool               `json:"hidden,omitempty"`
	Clipped   bool               `json:"clipped,omitempty"`
	ClippedBy []string           `json:"clippedBy,omitempty"`
	Disabled  bool               `json:"disabled,omitempty"`
	Focused   bool               `json:"focused,omitempty"`
	Selected  *bool              `json:"selected,omitempty"`
	Expanded  *bool              `json:"expanded,omitempty"`
	Pressed   *bool              `json:"pressed,omitempty"`
	Bounds    [4]int             `json:"bounds"`
	Scroll    *devSnapshotScroll `json:"scroll,omitempty"`
}

type devSnapshotScroll struct {
	Client [2]int `json:"client"`
	Size   [2]int `json:"size"`
	Offset [2]int `json:"offset"`
}

type devSnapshotAXValue struct {
	Value any `json:"value"`
}

type devSnapshotRawAXNode struct {
	NodeID      string             `json:"nodeId"`
	ParentID    string             `json:"parentId"`
	Ignored     bool               `json:"ignored"`
	Role        devSnapshotAXValue `json:"role"`
	Name        devSnapshotAXValue `json:"name"`
	Description devSnapshotAXValue `json:"description"`
	Properties  []struct {
		Name  string             `json:"name"`
		Value devSnapshotAXValue `json:"value"`
	} `json:"properties"`
}

type devSnapshotCDPCaller interface {
	Call(context.Context, string, any, any) error
}

func captureDevRendererSnapshot(
	ctx context.Context,
	cdp *businessacceptance.CDPClient,
) (devRendererSnapshot, error) {
	return captureDevRendererSnapshotWith(ctx, cdp)
}

func captureDevRendererSnapshotWith(
	ctx context.Context,
	cdp devSnapshotCDPCaller,
) (devRendererSnapshot, error) {
	var evaluated struct {
		Result struct {
			Value devRendererSnapshot `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": devRendererSnapshotExpression, "returnByValue": true, "awaitPromise": true,
	}, &evaluated); err != nil {
		return devRendererSnapshot{}, err
	}
	if len(evaluated.Exception) > 0 && string(evaluated.Exception) != "null" {
		return devRendererSnapshot{}, fmt.Errorf("renderer snapshot expression raised an exception")
	}

	var tree struct {
		Nodes []devSnapshotRawAXNode `json:"nodes"`
	}
	if err := cdp.Call(ctx, "Accessibility.getFullAXTree", map[string]any{}, &tree); err != nil {
		return devRendererSnapshot{}, err
	}
	snapshot := evaluated.Result.Value
	snapshot.Accessibility = normalizeDevAccessibility(tree.Nodes)
	snapshot.Summary = summarizeDevSnapshot(snapshot)
	return snapshot, nil
}

func normalizeDevAccessibility(nodes []devSnapshotRawAXNode) devSnapshotAccessibility {
	parents := make(map[string]string, len(nodes))
	for _, node := range nodes {
		parents[node.NodeID] = node.ParentID
	}
	result := devSnapshotAccessibility{Nodes: make([]devSnapshotAXNode, 0, min(len(nodes), maximumDevSnapshotNodes))}
	for _, node := range nodes {
		if node.Ignored {
			continue
		}
		role := snapshotValueString(node.Role.Value)
		name := snapshotValueString(node.Name.Value)
		if !includedSnapshotAXRole(role, name) {
			continue
		}
		if len(result.Nodes) == maximumDevSnapshotNodes {
			result.Truncated = true
			break
		}
		properties := make(map[string]any)
		for _, property := range node.Properties {
			if !includedSnapshotAXProperty(property.Name, property.Value.Value) {
				continue
			}
			properties[property.Name] = property.Value.Value
		}
		if len(properties) == 0 {
			properties = nil
		}
		result.Nodes = append(result.Nodes, devSnapshotAXNode{
			Depth:       snapshotAXDepth(node.ParentID, parents),
			Role:        role,
			Name:        name,
			Description: snapshotValueString(node.Description.Value),
			Properties:  properties,
		})
	}
	return result
}

func snapshotAXDepth(parent string, parents map[string]string) int {
	depth := 0
	visited := make(map[string]struct{})
	for parent != "" {
		if _, exists := visited[parent]; exists {
			break
		}
		visited[parent] = struct{}{}
		depth++
		parent = parents[parent]
	}
	return depth
}

func snapshotValueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func includedSnapshotAXRole(role, name string) bool {
	switch role {
	case "Audio", "RootWebArea", "alert", "article", "audio", "button", "checkbox", "combobox", "complementary",
		"dialog", "group", "heading", "link", "list", "listbox", "listitem", "main", "menu", "menuitem",
		"navigation", "progressbar", "radio", "radiogroup", "region", "separator", "slider", "spinbutton",
		"status", "switch", "tab", "tablist", "tabpanel", "textbox", "toolbar", "tree", "treeitem", "Video":
		return name != "" || role == "RootWebArea" || role == "main" || role == "navigation"
	default:
		return false
	}
}

func includedSnapshotAXProperty(name string, value any) bool {
	if value == nil || value == "" {
		return false
	}
	switch name {
	case "checked", "expanded", "level", "orientation", "pressed", "selected", "value", "valuetext":
		return true
	case "disabled", "focused", "invalid", "modal", "multiselectable", "readonly", "required":
		return value != false && value != "false"
	default:
		return false
	}
}

func summarizeDevSnapshot(snapshot devRendererSnapshot) devSnapshotSummary {
	summary := devSnapshotSummary{
		AccessibilityNodes: len(snapshot.Accessibility.Nodes),
		LayoutNodes:        len(snapshot.Layout.Nodes),
		PageOverflow:       snapshot.Document.OverflowX || snapshot.Document.OverflowY,
	}
	for _, node := range snapshot.Layout.Nodes {
		if !node.Hidden {
			summary.VisibleLayoutNodes++
		}
		if node.Clipped {
			summary.ClippedLayoutNodes++
		}
	}
	for _, node := range snapshot.Accessibility.Nodes {
		if node.Properties["disabled"] == true {
			summary.DisabledControls++
		}
	}
	return summary
}

func filterDevRendererSnapshot(snapshot devRendererSnapshot, match string) devRendererSnapshot {
	match = strings.TrimSpace(match)
	if match == "" {
		return snapshot
	}
	query := strings.ToLower(match)
	accessibility := snapshot.Accessibility.Nodes[:0]
	for _, node := range snapshot.Accessibility.Nodes {
		if strings.Contains(strings.ToLower(node.Role+" "+node.Name+" "+node.Description), query) {
			accessibility = append(accessibility, node)
		}
	}
	layout := snapshot.Layout.Nodes[:0]
	for _, node := range snapshot.Layout.Nodes {
		searchable := node.Tag + " " + node.Role + " " + node.Name + " " + strings.Join(node.ClippedBy, " ")
		if strings.Contains(strings.ToLower(searchable), query) {
			layout = append(layout, node)
		}
	}
	snapshot.Accessibility.Nodes = accessibility
	snapshot.Layout.Nodes = layout
	snapshot.Filter = &devSnapshotFilter{
		Match: match, AccessibilityNodes: len(accessibility), LayoutNodes: len(layout),
	}
	return snapshot
}

const devRendererSnapshotExpression = `(() => {
  const limit = 500;
  const clean = (value) => (value ?? "").replace(/\s+/g, " ").trim().slice(0, 120);
  const labelledText = (node) => clean((node.getAttribute("aria-labelledby") ?? "")
    .split(/\s+/)
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? "")
    .join(" "));
  const explicitName = (node) => clean(
    node.getAttribute("aria-label") ||
    labelledText(node) ||
    (node.labels ? [...node.labels].map((label) => label.textContent ?? "").join(" ") : "") ||
    node.getAttribute("alt") ||
    node.getAttribute("title")
  );
  const accessibleName = (node) => {
    const explicit = explicitName(node);
    if (explicit) return explicit;
    if (node.matches("button, a[href], [role=button], [role=tab], [role=status], [role=heading]")) {
      return clean(node.textContent);
    }
    return "";
  };
  const implicitRole = (node, name) => {
    const tag = node.tagName;
    if (tag === "MAIN") return "main";
    if (tag === "NAV") return "navigation";
    if (tag === "ASIDE") return "complementary";
    if (tag === "ARTICLE") return "article";
    if (tag === "HEADER") return "banner";
    if (tag === "FOOTER") return "contentinfo";
    if (tag === "SECTION" && name) return "region";
    if (tag === "BUTTON") return "button";
    if (tag === "A") return "link";
    if (tag === "TEXTAREA") return "textbox";
    if (tag === "SELECT") return "combobox";
    if (tag === "HR") return "separator";
    if (tag === "INPUT") {
      const type = node.type;
      if (type === "checkbox") return "checkbox";
      if (type === "radio") return "radio";
      if (type === "range") return "slider";
      if (type === "number") return "spinbutton";
      if (["button", "submit", "reset"].includes(type)) return "button";
      return "textbox";
    }
    return "";
  };
  const bounds = (rect) => [rect.x, rect.y, rect.width, rect.height].map(Math.round);
  const intersect = (left, right) => ({
    left: Math.max(left.left, right.left),
    top: Math.max(left.top, right.top),
    right: Math.min(left.right, right.right),
    bottom: Math.min(left.bottom, right.bottom),
  });
  const area = (rect) => Math.max(0, rect.right - rect.left) * Math.max(0, rect.bottom - rect.top);
  const clipDescriptor = (node) =>
    clean(node.getAttribute("aria-label") || node.getAttribute("role") || node.tagName.toLowerCase());
  const describe = (node) => {
    const rect = node.getBoundingClientRect();
    const style = getComputedStyle(node);
    const rendered = style.display !== "none" && style.visibility !== "hidden" &&
      Number(style.opacity) > 0 && rect.width > 0 && rect.height > 0;
    let visibleRect = {
      left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom,
    };
    const clippedBy = [];
    const viewport = { left: 0, top: 0, right: innerWidth, bottom: innerHeight };
    const viewportRect = intersect(visibleRect, viewport);
    if (area(viewportRect) + 0.5 < area(visibleRect)) clippedBy.push("viewport");
    visibleRect = viewportRect;
    for (let parent = node.parentElement; parent; parent = parent.parentElement) {
      const parentStyle = getComputedStyle(parent);
      if (!/(auto|scroll|hidden|clip)/.test(parentStyle.overflowX + " " + parentStyle.overflowY)) continue;
      const parentRect = parent.getBoundingClientRect();
      const next = intersect(visibleRect, parentRect);
      if (area(next) + 0.5 < area(visibleRect)) clippedBy.push(clipDescriptor(parent));
      visibleRect = next;
    }
    const visible = rendered && area(visibleRect) > 0.5;
    const name = accessibleName(node);
    const selected = node.hasAttribute("aria-selected") ? node.getAttribute("aria-selected") === "true" : null;
    const expanded = node.hasAttribute("aria-expanded") ? node.getAttribute("aria-expanded") === "true" : null;
    const pressed = node.hasAttribute("aria-pressed") ? node.getAttribute("aria-pressed") === "true" : null;
    const scroll = node.scrollWidth > node.clientWidth + 1 || node.scrollHeight > node.clientHeight + 1 ||
      node.scrollLeft !== 0 || node.scrollTop !== 0
      ? {
          client: [node.clientWidth, node.clientHeight],
          size: [node.scrollWidth, node.scrollHeight],
          offset: [node.scrollLeft, node.scrollTop],
        }
      : null;
    return {
      tag: node.tagName.toLowerCase(),
      role: node.getAttribute("role") || implicitRole(node, name),
      name,
      hidden: !visible,
      clipped: rendered && clippedBy.length > 0,
      clippedBy,
      disabled: node.matches(":disabled") || node.getAttribute("aria-disabled") === "true",
      focused: node === document.activeElement,
      selected,
      expanded,
      pressed,
      bounds: bounds(rect),
      scroll,
    };
  };
  const selector = [
    "main", "nav", "section", "aside", "article", "header", "footer",
    "button", "a[href]", "input", "textarea", "select", "video", "audio",
    "[role]", "[aria-label]",
  ].join(",");
  const candidates = [...document.querySelectorAll(selector)];
  const structuralRoles = new Set([
    "main", "navigation", "region", "complementary", "article", "banner", "contentinfo",
    "separator", "tablist", "tabpanel", "toolbar", "group",
  ]);
  const described = candidates.map(describe);
  const layoutNodes = described.filter((node) =>
    structuralRoles.has(node.role) || node.tag === "video" || node.tag === "audio" ||
    node.hidden || node.clipped || node.scroll || node.focused
  );
  const root = document.documentElement;
  const body = document.body;
  const clientWidth = root?.clientWidth ?? body?.clientWidth ?? 0;
  const clientHeight = root?.clientHeight ?? body?.clientHeight ?? 0;
  const scrollWidth = Math.max(root?.scrollWidth ?? 0, body?.scrollWidth ?? 0);
  const scrollHeight = Math.max(root?.scrollHeight ?? 0, body?.scrollHeight ?? 0);
  const active = document.activeElement;
  return {
    page: {
      url: location.href,
      title: document.title,
      readyState: document.readyState,
      visibilityState: document.visibilityState,
    },
    viewport: {
      outerWidth, outerHeight, innerWidth, innerHeight, devicePixelRatio,
    },
    document: {
      clientWidth, clientHeight, scrollWidth, scrollHeight,
      overflowX: scrollWidth > clientWidth + 1,
      overflowY: scrollHeight > clientHeight + 1,
    },
    focus: active && active !== body ? describe(active) : null,
    layout: {
      nodes: layoutNodes.slice(0, limit),
      truncated: layoutNodes.length > limit,
    },
  };
})()`
