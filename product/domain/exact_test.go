package domain

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

func TestExactIntegerJSONUsesCanonicalStrings(t *testing.T) {
	encoded, err := json.Marshal(NewInt64(math.MaxInt64))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"9223372036854775807"` {
		t.Fatalf("encoded int64 = %s", encoded)
	}
	for _, invalid := range []string{`1`, `"01"`, `"-0"`, `"+1"`, `"1e3"`, `"9223372036854775808"`} {
		var value Int64
		if err := json.Unmarshal([]byte(invalid), &value); err == nil {
			t.Fatalf("accepted invalid int64 %s", invalid)
		}
	}

	revision, err := NewRevision(math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"9223372036854775807"` {
		t.Fatalf("encoded revision = %s", encoded)
	}
	for _, invalid := range []string{`0`, `"-1"`, `"01"`, `"9223372036854775808"`} {
		var value Revision
		if err := json.Unmarshal([]byte(invalid), &value); err == nil {
			t.Fatalf("accepted invalid revision %s", invalid)
		}
	}
}

func TestRationalTimeNormalizationComparisonAndAddition(t *testing.T) {
	reduced, err := NewRationalTime(2002, 60000)
	if err != nil {
		t.Fatal(err)
	}
	if reduced.Value.Value() != 1001 || reduced.Scale != 30000 {
		t.Fatalf("reduced = %+v", reduced)
	}
	zero, err := NewRationalTime(0, 30000)
	if err != nil || zero.Scale != 1 {
		t.Fatalf("zero = %+v err=%v", zero, err)
	}
	if err := json.Unmarshal([]byte(`{"value":"2","scale":2}`), &reduced); err == nil {
		t.Fatal("accepted an unreduced wire RationalTime")
	}

	oneHalf, _ := NewRationalTime(1, 2)
	oneThird, _ := NewRationalTime(1, 3)
	comparison, err := oneHalf.Compare(oneThird)
	if err != nil || comparison <= 0 {
		t.Fatalf("comparison=%d err=%v", comparison, err)
	}
	sum, err := oneHalf.Add(oneThird)
	if err != nil || sum.Value.Value() != 5 || sum.Scale != 6 {
		t.Fatalf("sum=%+v err=%v", sum, err)
	}

	large, _ := NewRationalTime(math.MaxInt64, math.MaxInt32)
	if comparison, err := large.Compare(oneHalf); err != nil || comparison <= 0 {
		t.Fatalf("large comparison=%d err=%v", comparison, err)
	}
}

func TestExactRationalIsCanonicalAndDistinctFromTime(t *testing.T) {
	reduced, err := NewExactRational(-250, 1000)
	if err != nil || reduced.Value.Value() != -1 || reduced.Scale != 4 {
		t.Fatalf("reduced=%+v err=%v", reduced, err)
	}
	one, _ := NewExactRational(1, 1)
	if comparison, err := reduced.Compare(one); err != nil || comparison >= 0 {
		t.Fatalf("comparison=%d err=%v", comparison, err)
	}
	encoded, err := json.Marshal(reduced)
	if err != nil || string(encoded) != `{"value":"-1","scale":4}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
	for _, invalid := range []string{
		`{"value":"2","scale":2}`,
		`{"value":"0","scale":2}`,
		`{"value":"1","scale":1,"unit":"seconds"}`,
	} {
		var value ExactRational
		if err := json.Unmarshal([]byte(invalid), &value); err == nil {
			t.Fatalf("accepted invalid exact rational %s", invalid)
		}
	}
}

func TestUUIDv7GenerationAndTypedJSON(t *testing.T) {
	value, err := GenerateUUIDv7From(testInstant, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if !isUUIDv7(value) || value[14] != '7' || value[19] != '8' {
		t.Fatalf("UUIDv7 = %q", value)
	}
	projectID, err := ParseProjectID(value)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projectID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ProjectID
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != projectID {
		t.Fatalf("decoded=%v err=%v", decoded, err)
	}
	if _, err := ParseProjectID("018f0000-0000-7000-8000-00000000000A"); err == nil {
		t.Fatal("accepted uppercase durable ID")
	}
}
