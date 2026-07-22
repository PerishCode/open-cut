package toolchainclosure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const (
	qualificationReceiptSchema       = 1
	maximumQualificationReceiptBytes = 16 << 10
)

type qualificationReceipt struct {
	Schema      int    `json:"schema"`
	Profile     string `json:"profile"`
	InputSHA256 string `json:"inputSha256"`
	Outcome     string `json:"outcome"`
}

// WriteQualificationReceipt records that one owner-defined qualification
// profile succeeded for an exact, domain-separated input identity. The shared
// mechanism deliberately knows nothing about the toolchain or checks involved.
func WriteQualificationReceipt(root, name, profile, domain string, input any) error {
	if err := validateQualificationReceiptLocation(root, name, profile, domain); err != nil {
		return err
	}
	digest, err := ClosureDigest(domain, input)
	if err != nil {
		return fmt.Errorf("digest qualification input: %w", err)
	}
	return atomicfile.WriteJSON(filepath.Join(root, name), qualificationReceipt{
		Schema: qualificationReceiptSchema, Profile: profile, InputSHA256: digest, Outcome: "succeeded",
	}, 0o600)
}

// VerifyQualificationReceipt accepts a receipt only when its exact input
// digest matches what the current owner expects. Missing, malformed, stale, or
// linked files are ordinary verification failures; callers decide whether to
// replay the expensive qualification or fail closed.
func VerifyQualificationReceipt(root, name, profile, domain string, input any) error {
	if err := validateQualificationReceiptLocation(root, name, profile, domain); err != nil {
		return err
	}
	filename, err := ResolveContainedRegular(root, name)
	if err != nil {
		return fmt.Errorf("resolve qualification receipt: %w", err)
	}
	info, err := os.Lstat(filename)
	if err != nil || info.Size() <= 0 || info.Size() > maximumQualificationReceiptBytes {
		return fmt.Errorf("qualification receipt size is invalid")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read qualification receipt: %w", err)
	}
	var receipt qualificationReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("decode qualification receipt")
	}
	expected, err := ClosureDigest(domain, input)
	if err != nil {
		return fmt.Errorf("digest qualification input: %w", err)
	}
	if receipt.Schema != qualificationReceiptSchema || receipt.Profile != profile ||
		receipt.Outcome != "succeeded" || !ValidDigest(receipt.InputSHA256) ||
		receipt.InputSHA256 != expected {
		return fmt.Errorf("qualification receipt identity is invalid")
	}
	return nil
}

func validateQualificationReceiptLocation(root, name, profile, domain string) error {
	if !CleanAbsolute(root) || !ValidRelative(name) || filepath.Dir(name) != "." ||
		!Identifier.MatchString(profile) || strings.TrimSpace(domain) != domain || domain == "" ||
		len(domain) > 256 {
		return fmt.Errorf("qualification receipt contract is invalid")
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(physical) != root {
		return fmt.Errorf("qualification receipt root is invalid")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("qualification receipt root is invalid")
	}
	return nil
}
