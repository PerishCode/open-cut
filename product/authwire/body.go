package authwire

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

const MaximumCommandBodyBytes = 1 << 20

var (
	ErrInvalidCommandBody = errors.New("invalid command body")
	canonicalJSONInteger  = regexp.MustCompile(`^(0|-[1-9][0-9]*|[1-9][0-9]*)$`)
)

func CanonicalCommandBody(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaximumCommandBodyBytes {
		return nil, ErrInvalidCommandBody
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeStrictJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return nil, ErrInvalidCommandBody
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidCommandBody
	}
	return canonical, nil
}

func CommandBodyDigest(commandName string, raw []byte) (domain.Digest, error) {
	if _, err := command.InitialRegistry().Lookup(strings.Fields(commandName)); err != nil {
		return "", ErrInvalidCommandBody
	}
	payload := json.RawMessage("null")
	if raw != nil {
		canonical, err := CanonicalCommandBody(raw)
		if err != nil {
			return "", err
		}
		payload = canonical
	}
	canonical, err := json.Marshal(struct {
		Domain  string          `json:"domain"`
		Payload json.RawMessage `json:"payload"`
		Schema  string          `json:"schema"`
	}{
		Domain:  "open-cut/command-body/" + commandName,
		Payload: payload,
		Schema:  command.CommandSchemaVersion,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return domain.Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}

func NoBodyDigest(commandName string) string {
	digest, err := CommandBodyDigest(commandName, nil)
	if err != nil {
		panic(fmt.Sprintf("registered command has no body digest: %v", err))
	}
	return digest.String()
}

func decodeStrictJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrInvalidCommandBody
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, ErrInvalidCommandBody
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, ErrInvalidCommandBody
				}
				if _, duplicate := object[key]; duplicate {
					return nil, ErrInvalidCommandBody
				}
				decoded, err := decodeStrictJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				object[key] = decoded
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return nil, ErrInvalidCommandBody
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				decoded, err := decodeStrictJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				array = append(array, decoded)
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return nil, ErrInvalidCommandBody
			}
			return array, nil
		default:
			return nil, ErrInvalidCommandBody
		}
	case json.Number:
		if !canonicalJSONInteger.MatchString(value.String()) {
			return nil, ErrInvalidCommandBody
		}
		return value, nil
	case string, bool, nil:
		return value, nil
	default:
		return nil, ErrInvalidCommandBody
	}
}
