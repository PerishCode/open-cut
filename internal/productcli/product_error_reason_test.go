package productcli

import "testing"

func TestProductErrorReasonLiftsHumaDetail(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "detail and nested error",
			body: `{"title":"Unprocessable Entity","status":422,"detail":"edit request is invalid","errors":[{"message":"normalize EditProposal: edit request is invalid: operation 2 (insert-source-excerpt) is malformed"}]}`,
			want: "edit request is invalid: normalize EditProposal: edit request is invalid: operation 2 (insert-source-excerpt) is malformed",
		},
		{
			name: "detail only",
			body: `{"detail":"edit request is invalid"}`,
			want: "edit request is invalid",
		},
		{
			name: "nested error equal to detail is not duplicated",
			body: `{"detail":"boom","errors":[{"message":"boom"}]}`,
			want: "boom",
		},
		{"empty body", ``, ""},
		{"no detail", `{"title":"x","status":500}`, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := productErrorReason([]byte(testCase.body)); got != testCase.want {
				t.Fatalf("productErrorReason = %q, want %q", got, testCase.want)
			}
		})
	}
}
