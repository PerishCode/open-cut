package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"
)

var ErrInvalidCanonicalPayload = errors.New("invalid canonical payload")

// CanonicalEnvelope encodes a typed payload as the product's RFC 8785 subset.
// Product DTOs use only integer JSON numbers; exact large values are already
// decimal strings. Map keys are schema names or conservative local symbols.
func CanonicalEnvelope(domainName, schema string, payload any) ([]byte, error) {
	if domainName == "" || schema == "" {
		return nil, ErrInvalidCanonicalPayload
	}
	raw, err := json.Marshal(struct {
		Domain  string `json:"domain"`
		Payload any    `json:"payload"`
		Schema  string `json:"schema"`
	}{Domain: domainName, Payload: payload, Schema: schema})
	if err != nil {
		return nil, fmt.Errorf("marshal canonical payload: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidCanonicalPayload
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidCanonicalPayload
	}
	return appendCanonicalValue(nil, value)
}

func CanonicalDigest(domainName, schema string, payload any) ([]byte, Digest, error) {
	canonical, err := CanonicalEnvelope(domainName, schema, payload)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(canonical)
	return canonical, Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}

func appendCanonicalValue(buffer []byte, value any) ([]byte, error) {
	switch current := value.(type) {
	case nil:
		return append(buffer, "null"...), nil
	case bool:
		if current {
			return append(buffer, "true"...), nil
		}
		return append(buffer, "false"...), nil
	case string:
		if !utf8.ValidString(current) {
			return nil, ErrInvalidCanonicalPayload
		}
		return appendJCSString(buffer, current), nil
	case json.Number:
		value := current.String()
		if !isCanonicalDecimal(value, true) {
			return nil, ErrInvalidCanonicalPayload
		}
		return append(buffer, value...), nil
	case []any:
		buffer = append(buffer, '[')
		for index, item := range current {
			if index > 0 {
				buffer = append(buffer, ',')
			}
			var err error
			buffer, err = appendCanonicalValue(buffer, item)
			if err != nil {
				return nil, err
			}
		}
		return append(buffer, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer = append(buffer, '{')
		for index, key := range keys {
			if index > 0 {
				buffer = append(buffer, ',')
			}
			buffer = appendJCSString(buffer, key)
			buffer = append(buffer, ':')
			var err error
			buffer, err = appendCanonicalValue(buffer, current[key])
			if err != nil {
				return nil, err
			}
		}
		return append(buffer, '}'), nil
	default:
		return nil, ErrInvalidCanonicalPayload
	}
}
