package protocolgen

import (
	"encoding/json"
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
