package protocolgen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentifiers(t *testing.T) {
	cases := map[string]string{
		"sessionId":      "SessionID",
		"instanceId":     "InstanceID",
		"ttlSeconds":     "TTLSeconds",
		"url":            "URL",
		"prepare-latest": "PrepareLatest",
	}
	for input, expected := range cases {
		if actual := goIdentifier(input); actual != expected {
			t.Errorf("goIdentifier(%q)=%q want %q", input, actual, expected)
		}
	}
}

func TestMergedUnionKeepsOnlyCommonRequiredFields(t *testing.T) {
	document := openAPIDocument{}
	document.Components.Schemas = map[string]schema{
		"One": {Type: "object", Required: []string{"type", "name"}, Properties: map[string]schema{"type": {Type: "string"}, "name": {Type: "string"}}},
		"Two": {Type: "object", Required: []string{"type", "ready"}, Properties: map[string]schema{"type": {Type: "string"}, "ready": {Type: "boolean"}}},
	}
	merged, err := document.mergedUnion(schema{AnyOf: []schema{{Ref: "#/components/schemas/One"}, {Ref: "#/components/schemas/Two"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Required) != 1 || merged.Required[0] != "type" {
		t.Fatalf("required=%v", merged.Required)
	}
}

func TestNormalizeEventSchemaPromotesSidecarUnion(t *testing.T) {
	input := []byte(`{"$schema":"draft","$id":"events.schema.json","$defs":{"SidecarEvent":{"oneOf":[{"$ref":"ClientEvent.json"}]},"ClientEvent":{"$id":"ClientEvent.json","type":"object"}}}`)
	output, err := normalizeEventSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	if len(document["oneOf"].([]any)) != 1 {
		t.Fatalf("oneOf=%v", document["oneOf"])
	}
	definitions := document["$defs"].(map[string]any)
	if definitions["SidecarEvent"] != nil || definitions["ClientEvent"] == nil {
		t.Fatalf("$defs=%v", definitions)
	}
	ref := document["oneOf"].([]any)[0].(map[string]any)["$ref"]
	if ref != "#/$defs/ClientEvent" {
		t.Fatalf("$ref=%v", ref)
	}
	if definitions["ClientEvent"].(map[string]any)["$id"] != nil {
		t.Fatalf("embedded $id was not removed")
	}
}

func TestRoutesDeriveTransportSchemeFromOpenAPIMetadata(t *testing.T) {
	document := openAPIDocument{Paths: map[string]map[string]json.RawMessage{
		"/v1/status": {
			"get": json.RawMessage(`{"operationId":"status"}`),
		},
		"/v1/sessions/register": {
			"get": json.RawMessage(`{"operationId":"registerSession","x-transport":"websocket"}`),
		},
	}}
	document.Servers = append(document.Servers, struct {
		URL string `json:"url"`
	}{URL: "http://127.0.0.1:{port}"})
	routes, err := document.routes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes=%v", routes)
	}
	if routes[0].OperationID != "registerSession" || routes[0].Scheme != "ws" {
		t.Fatalf("register route=%+v", routes[0])
	}
	if routes[1].OperationID != "status" || routes[1].Scheme != "http" {
		t.Fatalf("status route=%+v", routes[1])
	}
}

func TestTypeScriptBindingsExposeGeneratedDecoders(t *testing.T) {
	document := openAPIDocument{Paths: map[string]map[string]json.RawMessage{
		"/v1/status": {"get": json.RawMessage(`{"operationId":"status"}`)},
	}}
	document.Servers = append(document.Servers, struct {
		URL string `json:"url"`
	}{URL: "http://127.0.0.1:{port}"})
	document.Components.Schemas = map[string]schema{
		"ControlDescriptor": {
			Type: "object", Required: []string{"protocol"},
			Properties: map[string]schema{"protocol": {Type: "string", Enum: []any{"sidecar.v1"}}},
		},
		"SidecarLaunch": {
			Type: "object", Required: []string{"control"},
			Properties: map[string]schema{
				"control": {Ref: "#/components/schemas/ControlDescriptor", Environment: "OC_SIDECAR_CONTROL"},
			},
		},
		"Status": {
			Type: "object", Required: []string{"revision"},
			Properties: map[string]schema{"revision": {Type: "integer", Minimum: float64Pointer(0)}},
		},
	}
	generated, err := generateTypeScript(document)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		`scheme: "http"`,
		`export const protocolSchemas`,
		`export function decodeSidecarLaunch(value: unknown): SidecarLaunch`,
		`export function decodeStatus(value: unknown): Status`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("generated TypeScript does not contain %q\n%s", expected, text)
		}
	}
}

func float64Pointer(value float64) *float64 { return &value }
