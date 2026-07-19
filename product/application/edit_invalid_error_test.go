package application

import (
	"errors"
	"testing"
)

func TestEditInvalidErrorUnwrapsAndCarriesReason(t *testing.T) {
	err := editInvalidf("operation %d (%s) is malformed", 2, "insert-source-excerpt")
	if !errors.Is(err, ErrEditInvalid) {
		t.Fatal("EditInvalidError must unwrap to ErrEditInvalid so existing status mapping holds")
	}
	want := "edit request is invalid: operation 2 (insert-source-excerpt) is malformed"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	var invalid EditInvalidError
	if !errors.As(err, &invalid) || invalid.Reason == "" {
		t.Fatalf("EditInvalidError reason was not carried: %+v", invalid)
	}
	bare := EditInvalidError{}
	if bare.Error() != ErrEditInvalid.Error() {
		t.Fatalf("reasonless EditInvalidError = %q, want %q", bare.Error(), ErrEditInvalid.Error())
	}
}
