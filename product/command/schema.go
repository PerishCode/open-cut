package command

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	exactSignedPattern   = `^(0|-[1-9][0-9]*|[1-9][0-9]*)$`
	exactUnsignedPattern = `^(0|[1-9][0-9]*)$`
	uuidV7Pattern        = `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
)

var ErrUnsupportedSchemaType = errors.New("unsupported command schema type")

type JSONSchema struct {
	Type                 string                 `json:"type,omitempty"`
	Format               string                 `json:"format,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Pattern              string                 `json:"pattern,omitempty"`
	Properties           map[string]*JSONSchema `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	AdditionalProperties *bool                  `json:"additionalProperties,omitempty"`
	Items                *JSONSchema            `json:"items,omitempty"`
	Enum                 []string               `json:"enum,omitempty"`
	Minimum              *int64                 `json:"minimum,omitempty"`
	Maximum              *int64                 `json:"maximum,omitempty"`
	MinLength            *int                   `json:"minLength,omitempty"`
	MaxLength            *int                   `json:"maxLength,omitempty"`
	MinItems             *int                   `json:"minItems,omitempty"`
	MaxItems             *int                   `json:"maxItems,omitempty"`
	Default              any                    `json:"default,omitempty"`
}

func SchemaFor[Value any]() (*JSONSchema, error) {
	return schemaFor(reflect.TypeFor[Value]())
}

func schemaFor(valueType reflect.Type) (*JSONSchema, error) {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if schema := exactSchema(valueType); schema != nil {
		return schema, nil
	}
	if valueType == reflect.TypeFor[time.Time]() {
		return &JSONSchema{Type: "string", Format: "date-time"}, nil
	}
	if implementsText(valueType) {
		return &JSONSchema{Type: "string"}, nil
	}
	switch valueType.Kind() {
	case reflect.Bool:
		return &JSONSchema{Type: "boolean"}, nil
	case reflect.String:
		return &JSONSchema{Type: "string"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &JSONSchema{Type: "integer"}, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaFor(valueType.Elem())
		if err != nil {
			return nil, err
		}
		return &JSONSchema{Type: "array", Items: items}, nil
	case reflect.Struct:
		return schemaForStruct(valueType)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSchemaType, valueType)
	}
}

func exactSchema(valueType reflect.Type) *JSONSchema {
	if valueType.PkgPath() != reflect.TypeFor[domain.Revision]().PkgPath() {
		return nil
	}
	switch {
	case valueType == reflect.TypeFor[domain.Int64]():
		return &JSONSchema{Type: "string", Format: "int64-decimal", Pattern: exactSignedPattern}
	case valueType == reflect.TypeFor[domain.Revision]():
		return &JSONSchema{Type: "string", Format: "uint64-decimal", Pattern: exactUnsignedPattern}
	case valueType == reflect.TypeFor[domain.Cursor]():
		return &JSONSchema{Type: "string", Format: "uint64-decimal", Pattern: exactUnsignedPattern}
	case strings.HasPrefix(valueType.Name(), "ID["):
		return &JSONSchema{Type: "string", Format: "uuid", Pattern: uuidV7Pattern}
	default:
		return nil
	}
}

func schemaForStruct(valueType reflect.Type) (*JSONSchema, error) {
	allowAdditional := false
	schema := &JSONSchema{
		Type: "object", Properties: make(map[string]*JSONSchema),
		AdditionalProperties: &allowAdditional,
	}
	for index := 0; index < valueType.NumField(); index++ {
		field := valueType.Field(index)
		if !field.IsExported() {
			continue
		}
		name, optional, ignored := jsonField(field)
		if ignored {
			continue
		}
		fieldSchema, err := schemaFor(field.Type)
		if err != nil {
			return nil, fmt.Errorf("schema field %s.%s: %w", valueType, field.Name, err)
		}
		if err := applySchemaTags(fieldSchema, field); err != nil {
			return nil, fmt.Errorf("schema field %s.%s: %w", valueType, field.Name, err)
		}
		schema.Properties[name] = fieldSchema
		if !optional && field.Type.Kind() != reflect.Pointer {
			schema.Required = append(schema.Required, name)
		}
	}
	sort.Strings(schema.Required)
	return schema, nil
}

func applySchemaTags(schema *JSONSchema, field reflect.StructField) error {
	schema.Description = field.Tag.Get("doc")
	if enum := field.Tag.Get("enum"); enum != "" {
		schema.Enum = strings.Split(enum, ",")
	}
	var err error
	if schema.Minimum, err = parseInt64Tag(field, "minimum"); err != nil {
		return err
	}
	if schema.Maximum, err = parseInt64Tag(field, "maximum"); err != nil {
		return err
	}
	if schema.MinLength, err = parseIntTag(field, "minLength"); err != nil {
		return err
	}
	if schema.MaxLength, err = parseIntTag(field, "maxLength"); err != nil {
		return err
	}
	if schema.MinItems, err = parseIntTag(field, "minItems"); err != nil {
		return err
	}
	if schema.MaxItems, err = parseIntTag(field, "maxItems"); err != nil {
		return err
	}
	if value := field.Tag.Get("default"); value != "" {
		switch schema.Type {
		case "integer":
			parsed, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			schema.Default = parsed
		default:
			schema.Default = value
		}
	}
	return nil
}

func jsonField(field reflect.StructField) (name string, optional, ignored bool) {
	tag := field.Tag.Get("json")
	parts := strings.Split(tag, ",")
	if parts[0] == "-" {
		return "", false, true
	}
	name = parts[0]
	if name == "" {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			optional = true
		}
	}
	return name, optional, false
}

func parseInt64Tag(field reflect.StructField, name string) (*int64, error) {
	value := field.Tag.Get(name)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseIntTag(field reflect.StructField, name string) (*int, error) {
	value := field.Tag.Get(name)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func implementsText(valueType reflect.Type) bool {
	unmarshaler := reflect.TypeFor[encoding.TextUnmarshaler]()
	return valueType.Implements(unmarshaler) || reflect.PointerTo(valueType).Implements(unmarshaler)
}
