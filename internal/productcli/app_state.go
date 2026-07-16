package productcli

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	envProjectID  = "OPEN_CUT_PROJECT_ID"
	envSequenceID = "OPEN_CUT_SEQUENCE_ID"
	envRunID      = "OPEN_CUT_RUN_ID"
	envTurnID     = "OPEN_CUT_TURN_ID"
	envOutput     = "OPEN_CUT_OUTPUT"
	envWaitMS     = "OPEN_CUT_WAIT_MS"
)

type appStateFlags struct {
	projectID  *string
	sequenceID *string
	runID      *string
	turnID     *string
	output     *string
	waitMS     *string
}

type resolvedAppState struct {
	context        command.Context
	policyOverride application.InvocationPolicyOverride
}

func addAppStateFlags(set *flag.FlagSet) appStateFlags {
	return appStateFlags{
		projectID:  set.String("project-id", os.Getenv(envProjectID), "Project context; overrides OPEN_CUT_PROJECT_ID"),
		sequenceID: set.String("sequence-id", os.Getenv(envSequenceID), "Sequence context; overrides OPEN_CUT_SEQUENCE_ID"),
		runID:      set.String("run-id", os.Getenv(envRunID), "AgentRun context; overrides OPEN_CUT_RUN_ID"),
		turnID:     set.String("turn-id", os.Getenv(envTurnID), "AgentTurn context; overrides OPEN_CUT_TURN_ID"),
		output:     set.String("output", os.Getenv(envOutput), "output mode (json or human)"),
		waitMS:     set.String("wait-ms", os.Getenv(envWaitMS), "bounded wait in milliseconds (250-30000)"),
	}
}

func (flags appStateFlags) Resolve() (resolvedAppState, error) {
	var override application.InvocationPolicyOverride
	if *flags.output != "" {
		value := application.OutputMode(*flags.output)
		if value != application.OutputJSON && value != application.OutputHuman {
			return resolvedAppState{}, fmt.Errorf("invalid output mode")
		}
		override.Output = &value
	}
	if *flags.waitMS != "" {
		parsed, err := strconv.ParseUint(*flags.waitMS, 10, 32)
		if err != nil {
			return resolvedAppState{}, fmt.Errorf("invalid bounded wait")
		}
		value := uint32(parsed)
		if value < application.MinimumWaitMilliseconds || value > application.MaximumWaitMilliseconds {
			return resolvedAppState{}, fmt.Errorf("invalid bounded wait")
		}
		override.WaitMilliseconds = &value
	}
	var result command.Context
	if *flags.projectID != "" {
		value, err := domain.ParseProjectID(*flags.projectID)
		if err != nil {
			return resolvedAppState{}, fmt.Errorf("invalid project context")
		}
		result.ProjectID = &value
	}
	if *flags.sequenceID != "" {
		value, err := domain.ParseSequenceID(*flags.sequenceID)
		if err != nil {
			return resolvedAppState{}, fmt.Errorf("invalid sequence context")
		}
		result.SequenceID = &value
	}
	if *flags.runID != "" {
		value, err := domain.ParseRunID(*flags.runID)
		if err != nil {
			return resolvedAppState{}, fmt.Errorf("invalid run context")
		}
		result.RunID = &value
	}
	if *flags.turnID != "" {
		value, err := domain.ParseTurnID(*flags.turnID)
		if err != nil {
			return resolvedAppState{}, fmt.Errorf("invalid turn context")
		}
		result.TurnID = &value
	}
	return resolvedAppState{context: result, policyOverride: override}, nil
}
