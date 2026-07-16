package domain

import "errors"

const RoughCutDerivationSchema = "open-cut/rough-cut-derivation/v1"

var ErrInvalidRoughCutPolicy = errors.New("invalid rough-cut derivation policy")

// RoughCutDerivationPolicy makes every otherwise tempting editor default part
// of the reviewed operation. A future layout behavior requires a new policy ID.
type RoughCutDerivationPolicy struct {
	ID              string       `json:"id" enum:"paper-edit-rough-cut-v1"`
	Ordering        string       `json:"ordering" enum:"request-order"`
	InterExcerptGap RationalTime `json:"interExcerptGap"`
	SourceHandles   string       `json:"sourceHandles" enum:"zero"`
	Rate            string       `json:"rate" enum:"1:1"`
	Overwrite       string       `json:"overwrite" enum:"forbidden"`
	AVGrouping      string       `json:"avGrouping" enum:"one-link-group-per-two-lane-excerpt"`
}

func PaperEditRoughCutPolicyV1() RoughCutDerivationPolicy {
	zero, _ := NewRationalTime(0, 1)
	return RoughCutDerivationPolicy{
		ID: "paper-edit-rough-cut-v1", Ordering: "request-order", InterExcerptGap: zero,
		SourceHandles: "zero", Rate: "1:1", Overwrite: "forbidden",
		AVGrouping: "one-link-group-per-two-lane-excerpt",
	}
}

func (policy RoughCutDerivationPolicy) Validate() error {
	expected := PaperEditRoughCutPolicyV1()
	if policy.ID != expected.ID || policy.Ordering != expected.Ordering ||
		policy.InterExcerptGap != expected.InterExcerptGap || policy.SourceHandles != expected.SourceHandles ||
		policy.Rate != expected.Rate || policy.Overwrite != expected.Overwrite ||
		policy.AVGrouping != expected.AVGrouping {
		return ErrInvalidRoughCutPolicy
	}
	return nil
}
