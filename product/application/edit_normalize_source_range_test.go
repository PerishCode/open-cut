package application

import (
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSourceRangeWithinUsesAbsoluteStreamCoverage(t *testing.T) {
	nonzeroStart := testRational(t, 10, 1)
	duration := testRational(t, 5, 1)
	negativeStart := testRational(t, -2, 1)

	tests := []struct {
		name       string
		rangeValue domain.TimeRange
		descriptor domain.SourceStreamDescriptor
		want       bool
	}{
		{
			name:       "nonzero coverage start",
			rangeValue: testTimeRange(t, 10, 1, 5, 1),
			descriptor: domain.SourceStreamDescriptor{StartTime: &nonzeroStart, Duration: &duration},
			want:       true,
		},
		{
			name:       "zero is before nonzero coverage",
			rangeValue: testTimeRange(t, 0, 1, 5, 1),
			descriptor: domain.SourceStreamDescriptor{StartTime: &nonzeroStart, Duration: &duration},
			want:       false,
		},
		{
			name:       "end exceeds coverage",
			rangeValue: testTimeRange(t, 14, 1, 2, 1),
			descriptor: domain.SourceStreamDescriptor{StartTime: &nonzeroStart, Duration: &duration},
			want:       false,
		},
		{
			name:       "negative absolute coverage",
			rangeValue: testTimeRange(t, -2, 1, 2, 1),
			descriptor: domain.SourceStreamDescriptor{StartTime: &negativeStart, Duration: &duration},
			want:       true,
		},
		{
			name:       "absent start means zero",
			rangeValue: testTimeRange(t, 0, 1, 1, 1),
			descriptor: domain.SourceStreamDescriptor{Duration: &duration},
			want:       true,
		},
		{
			name:       "absent duration has no finite upper bound",
			rangeValue: testTimeRange(t, 100, 1, 1, 1),
			descriptor: domain.SourceStreamDescriptor{StartTime: &nonzeroStart},
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sourceRangeWithin(test.rangeValue, test.descriptor); got != test.want {
				t.Fatalf("sourceRangeWithin()=%v, want %v", got, test.want)
			}
		})
	}
}
