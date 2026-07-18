package application

import "testing"

func TestBoundedDiagnosticDetailNormalizesAndBounds(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"collapses newlines and tabs", "encode failed:\n\tffmpeg: bad frame\r\n", "encode failed: ffmpeg: bad frame"},
		{"trims and collapses runs", "   a    b   ", "a b"},
		{"strips control bytes", "raw\x00video\x01byte", "raw video byte"},
		{"repairs invalid utf8", "good\xff\xfetail", "goodtail"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := BoundedDiagnosticDetail(testCase.input); got != testCase.want {
				t.Fatalf("BoundedDiagnosticDetail(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
	long := make([]byte, MaximumDiagnosticDetail*2)
	for index := range long {
		long[index] = 'x'
	}
	bounded := BoundedDiagnosticDetail(string(long))
	if len(bounded) != MaximumDiagnosticDetail {
		t.Fatalf("bounded length = %d, want %d", len(bounded), MaximumDiagnosticDetail)
	}
}
