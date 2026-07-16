package productcli

import (
	"flag"
	"fmt"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func parseSequenceFramesInvocation(
	set *flag.FlagSet,
	args []string,
) (command.SequenceFramesInput, error) {
	revisionValue := set.String("sequence-revision", "", "exact committed Sequence revision")
	jobValue := set.String("job-id", "", "frame job lineage to continue")
	retryValue := set.String("retry-job-id", "", "recoverable terminal frame job lineage to retry")
	var timeArguments stringListFlag
	set.Var(&timeArguments, "time", "exact Sequence time as value/scale seconds; repeat one to eight times")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return command.SequenceFramesInput{}, fmt.Errorf("invalid sequence frames invocation")
	}
	modeCount := 0
	if *revisionValue != "" || len(timeArguments) != 0 {
		modeCount++
	}
	if *jobValue != "" {
		modeCount++
	}
	if *retryValue != "" {
		modeCount++
	}
	if modeCount != 1 {
		return command.SequenceFramesInput{}, fmt.Errorf("sequence frames requires exactly one prepare, continue, or retry operation")
	}
	input := command.SequenceFramesInput{}
	switch {
	case *revisionValue != "" || len(timeArguments) != 0:
		if *revisionValue == "" || len(timeArguments) == 0 ||
			len(timeArguments) > application.MaximumSequenceFrameSamples {
			return command.SequenceFramesInput{}, fmt.Errorf("sequence frames prepare requires revision and one to eight times")
		}
		var revision domain.Revision
		if err := revision.UnmarshalText([]byte(*revisionValue)); err != nil || revision.Value() == 0 {
			return command.SequenceFramesInput{}, fmt.Errorf("invalid Sequence revision")
		}
		input.SequenceRevision = &revision
		input.Times = make([]domain.RationalTime, 0, len(timeArguments))
		for _, value := range timeArguments {
			instant, err := parseRationalArgument(value, false)
			if err != nil || instant.IsNegative() {
				return command.SequenceFramesInput{}, fmt.Errorf("invalid Sequence frame time")
			}
			if len(input.Times) > 0 {
				comparison, compareErr := input.Times[len(input.Times)-1].Compare(instant)
				if compareErr != nil || comparison >= 0 {
					return command.SequenceFramesInput{}, fmt.Errorf("Sequence frame times must be strictly increasing")
				}
			}
			input.Times = append(input.Times, instant)
		}
	case *jobValue != "":
		jobID, err := domain.ParseWorkJobID(*jobValue)
		if err != nil {
			return command.SequenceFramesInput{}, fmt.Errorf("invalid sequence frame job identity")
		}
		input.JobID = &jobID
	case *retryValue != "":
		jobID, err := domain.ParseWorkJobID(*retryValue)
		if err != nil {
			return command.SequenceFramesInput{}, fmt.Errorf("invalid sequence frame retry identity")
		}
		input.RetryJobID = &jobID
	}
	if _, err := input.ApplicationInput(); err != nil {
		return command.SequenceFramesInput{}, fmt.Errorf("invalid sequence frames invocation")
	}
	return input, nil
}
