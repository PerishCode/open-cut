package protocolgen

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
)

type openAPIDocument struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]schema `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	OperationID string `json:"operationId"`
	Transport   string `json:"x-transport"`
}

type schema struct {
	Ref                  string            `json:"$ref,omitempty"`
	Type                 string            `json:"type,omitempty"`
	Format               string            `json:"format,omitempty"`
	Enum                 []any             `json:"enum,omitempty"`
	AnyOf                []schema          `json:"anyOf,omitempty"`
	OneOf                []schema          `json:"oneOf,omitempty"`
	AllOf                []schema          `json:"allOf,omitempty"`
	Properties           map[string]schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	Items                *schema           `json:"items,omitempty"`
	Minimum              *float64          `json:"minimum,omitempty"`
	Maximum              *float64          `json:"maximum,omitempty"`
	MinLength            *int              `json:"minLength,omitempty"`
	MaxLength            *int              `json:"maxLength,omitempty"`
	UniqueItems          bool              `json:"uniqueItems,omitempty"`
	Environment          string            `json:"x-environment,omitempty"`
	AdditionalProperties any               `json:"additionalProperties,omitempty"`
}

type route struct {
	OperationID string
	Method      string
	Path        string
	Scheme      string
}

type environmentBinding struct {
	Property string
	Variable string
}

func parseOpenAPI(data []byte) (openAPIDocument, error) {
	var document openAPIDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return openAPIDocument{}, fmt.Errorf("decode generated OpenAPI: %w", err)
	}
	if document.Info.Version == "" || len(document.Paths) == 0 || len(document.Components.Schemas) == 0 {
		return openAPIDocument{}, fmt.Errorf("generated OpenAPI is missing version, paths, or schemas")
	}
	return document, nil
}

func normalizeEventSchema(data []byte) ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode generated event schema: %w", err)
	}
	definitions, ok := document["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("generated event schema has no $defs")
	}
	root, ok := definitions["SidecarEvent"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("generated event schema has no SidecarEvent definition")
	}
	oneOf, ok := root["oneOf"].([]any)
	if !ok || len(oneOf) == 0 {
		return nil, fmt.Errorf("generated SidecarEvent schema has no oneOf")
	}
	document["oneOf"] = oneOf
	delete(definitions, "SidecarEvent")
	for _, definition := range definitions {
		if object, ok := definition.(map[string]any); ok {
			delete(object, "$schema")
			delete(object, "$id")
		}
	}
	rewriteBundledReferences(document, definitions)
	normalized, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode normalized event schema: %w", err)
	}
	return append(normalized, '\n'), nil
}

func rewriteBundledReferences(value any, definitions map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok {
			name := strings.TrimSuffix(path.Base(ref), ".json")
			if definitions[name] != nil {
				typed["$ref"] = "#/$defs/" + name
			}
		}
		for _, child := range typed {
			rewriteBundledReferences(child, definitions)
		}
	case []any:
		for _, child := range typed {
			rewriteBundledReferences(child, definitions)
		}
	}
}

func (document openAPIDocument) routes() ([]route, error) {
	requestScheme, err := document.requestScheme()
	if err != nil {
		return nil, err
	}
	routes := make([]route, 0, len(document.Paths))
	for routePath, item := range document.Paths {
		for method, raw := range item {
			if method != "get" && method != "post" && method != "put" && method != "patch" && method != "delete" {
				continue
			}
			var candidate operation
			if err := json.Unmarshal(raw, &candidate); err != nil {
				return nil, fmt.Errorf("decode %s %s: %w", method, routePath, err)
			}
			if candidate.OperationID == "" {
				return nil, fmt.Errorf("%s %s has no operationId", method, routePath)
			}
			scheme := requestScheme
			switch candidate.Transport {
			case "":
			case "websocket":
				if requestScheme == "https" {
					scheme = "wss"
				} else {
					scheme = "ws"
				}
			default:
				return nil, fmt.Errorf("%s %s has unsupported x-transport %q", method, routePath, candidate.Transport)
			}
			routes = append(routes, route{OperationID: candidate.OperationID, Method: strings.ToUpper(method), Path: routePath, Scheme: scheme})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].OperationID < routes[j].OperationID })
	return routes, nil
}

func (document openAPIDocument) requestScheme() (string, error) {
	if len(document.Servers) == 0 {
		return "", fmt.Errorf("generated OpenAPI has no server URL")
	}
	scheme, _, found := strings.Cut(document.Servers[0].URL, "://")
	if !found || scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("generated OpenAPI has unsupported server URL %q", document.Servers[0].URL)
	}
	return scheme, nil
}

func (document openAPIDocument) schemaNames() []string {
	names := make([]string, 0, len(document.Components.Schemas))
	for name := range document.Components.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (document openAPIDocument) protocolVersion() (string, error) {
	descriptor, ok := document.Components.Schemas["ControlDescriptor"]
	if !ok {
		return "", fmt.Errorf("ControlDescriptor schema is missing")
	}
	protocolField, ok := descriptor.Properties["protocol"]
	if !ok || len(protocolField.Enum) != 1 {
		return "", fmt.Errorf("ControlDescriptor.protocol must be one literal")
	}
	version, ok := protocolField.Enum[0].(string)
	if !ok || version == "" {
		return "", fmt.Errorf("ControlDescriptor.protocol must be a string literal")
	}
	return version, nil
}

func (document openAPIDocument) eventValues() []string {
	seen := make(map[string]bool)
	for _, candidate := range document.Components.Schemas {
		field, ok := candidate.Properties["type"]
		if !ok || len(field.Enum) != 1 {
			continue
		}
		value, ok := field.Enum[0].(string)
		if ok && value != "" {
			seen[value] = true
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func (document openAPIDocument) environmentBindings() ([]environmentBinding, error) {
	launch, ok := document.Components.Schemas["SidecarLaunch"]
	if !ok || launch.Type != "object" {
		return nil, fmt.Errorf("SidecarLaunch schema is missing")
	}
	bindings := make([]environmentBinding, 0, len(launch.Properties))
	seen := make(map[string]bool)
	for property, candidate := range launch.Properties {
		if candidate.Environment == "" {
			return nil, fmt.Errorf("SidecarLaunch.%s has no x-environment binding", property)
		}
		if seen[candidate.Environment] {
			return nil, fmt.Errorf("SidecarLaunch repeats environment variable %s", candidate.Environment)
		}
		seen[candidate.Environment] = true
		bindings = append(bindings, environmentBinding{Property: property, Variable: candidate.Environment})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Property < bindings[j].Property })
	return bindings, nil
}

func (document openAPIDocument) unionMembers(candidate schema) ([]schema, error) {
	variants := candidate.AnyOf
	if len(variants) == 0 {
		variants = candidate.OneOf
	}
	var members []schema
	for _, variant := range variants {
		resolved, err := document.resolve(variant)
		if err != nil {
			return nil, err
		}
		if len(resolved.AnyOf) > 0 || len(resolved.OneOf) > 0 {
			nested, err := document.unionMembers(resolved)
			if err != nil {
				return nil, err
			}
			members = append(members, nested...)
			continue
		}
		members = append(members, resolved)
	}
	return members, nil
}

func (document openAPIDocument) mergedUnion(candidate schema) (schema, error) {
	members, err := document.unionMembers(candidate)
	if err != nil {
		return schema{}, err
	}
	if len(members) == 0 {
		return schema{}, fmt.Errorf("union has no members")
	}
	merged := schema{Type: "object", Properties: make(map[string]schema)}
	requiredCount := make(map[string]int)
	for _, member := range members {
		if member.Type != "object" {
			return schema{}, fmt.Errorf("union member is %q, want object", member.Type)
		}
		memberRequired := stringSet(member.Required)
		for name, property := range member.Properties {
			if _, exists := merged.Properties[name]; !exists {
				merged.Properties[name] = property
			}
			if memberRequired[name] {
				requiredCount[name]++
			}
		}
	}
	for name, count := range requiredCount {
		if count == len(members) {
			merged.Required = append(merged.Required, name)
		}
	}
	sort.Strings(merged.Required)
	return merged, nil
}

func (document openAPIDocument) resolve(candidate schema) (schema, error) {
	if candidate.Ref == "" {
		return candidate, nil
	}
	name := path.Base(candidate.Ref)
	resolved, ok := document.Components.Schemas[name]
	if !ok {
		return schema{}, fmt.Errorf("schema reference %q is missing", candidate.Ref)
	}
	return resolved, nil
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func referenceName(ref string) string {
	return path.Base(ref)
}

var initialisms = map[string]string{
	"api":  "API",
	"http": "HTTP",
	"id":   "ID",
	"pid":  "PID",
	"ttl":  "TTL",
	"url":  "URL",
}

func goIdentifier(value string) string {
	words := identifierWords(value)
	var result strings.Builder
	for _, word := range words {
		if initialism, ok := initialisms[strings.ToLower(word)]; ok {
			result.WriteString(initialism)
			continue
		}
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
			result.WriteString(string(runes))
		}
	}
	return result.String()
}

func typescriptIdentifier(value string) string {
	goName := goIdentifier(value)
	if goName == "" {
		return ""
	}
	runes := []rune(goName)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func identifierWords(value string) []string {
	var words []string
	start := -1
	runes := []rune(value)
	flush := func(end int) {
		if start >= 0 && end > start {
			words = append(words, string(runes[start:end]))
		}
		start = -1
	}
	for index, current := range runes {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			flush(index)
			continue
		}
		if start < 0 {
			start = index
			continue
		}
		previous := runes[index-1]
		if unicode.IsLower(previous) && unicode.IsUpper(current) || unicode.IsLetter(previous) && unicode.IsDigit(current) || unicode.IsDigit(previous) && unicode.IsLetter(current) {
			flush(index)
			start = index
			continue
		}
		if unicode.IsUpper(previous) && unicode.IsUpper(current) && index+1 < len(runes) && unicode.IsLower(runes[index+1]) {
			flush(index)
			start = index
		}
	}
	flush(len(runes))
	return words
}

func literal(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
