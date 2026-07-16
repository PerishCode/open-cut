package productcli

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func parseExportInvocation(
	name string,
	set *flag.FlagSet,
	args []string,
) (any, domain.WorkJobID, error) {
	switch name {
	case "export start":
		input, err := parseExportStartInvocation(set, args)
		return input, domain.WorkJobID{}, err
	case "export show":
		jobID, err := parseExportJobInvocation(set, args)
		return nil, jobID, err
	case "export retry":
		jobID, err := parseExportJobInvocation(set, args)
		return command.ExportRetryInput{JobID: jobID}, jobID, err
	case "export cancel":
		input, err := parseExportCancelInvocation(set, args)
		return input, input.JobID, err
	default:
		return nil, domain.WorkJobID{}, command.ErrUnknownCommand
	}
}

func exportInvocationPath(
	name string,
	state command.Context,
	jobID domain.WorkJobID,
) (string, error) {
	if state.ProjectID == nil || state.RunID == nil || state.TurnID == nil {
		return "", fmt.Errorf("%s requires project, run, and turn context", name)
	}
	prefix := "/v1/projects/" + state.ProjectID.String() + "/runs/" +
		state.RunID.String() + "/turns/" + state.TurnID.String()
	if name == "export start" {
		if state.SequenceID == nil {
			return "", fmt.Errorf("export start requires project, sequence, run, and turn context")
		}
		return prefix + "/sequences/" + state.SequenceID.String() + "/exports", nil
	}
	path := prefix + "/exports/" + jobID.String()
	if name == "export retry" {
		path += "/retry"
	} else if name == "export cancel" {
		path += "/cancel"
	}
	return path, nil
}

func exportResultStatus(name string, raw []byte) (command.Status, error) {
	var export command.ExportData
	if err := json.Unmarshal(raw, &export); err != nil || export.Validate() != nil {
		return command.StatusFailed, fmt.Errorf("invalid export response")
	}
	switch export.Job.State {
	case domain.MediaJobBlocked, domain.MediaJobQueued, domain.MediaJobRunning:
		return command.StatusAccepted, nil
	case domain.MediaJobFailed:
		return command.StatusFailed, nil
	case domain.MediaJobCancelled:
		if name == "export cancel" {
			return command.StatusSucceeded, nil
		}
		return command.StatusFailed, nil
	default:
		return command.StatusSucceeded, nil
	}
}

func parseExportStartInvocation(set *flag.FlagSet, args []string) (command.ExportStartInput, error) {
	request := set.String("request-id", "", "idempotent export request identity")
	revisionValue := set.String("sequence-revision", "", "exact committed Sequence revision")
	preset := set.String("preset", "", "immutable export preset")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return command.ExportStartInput{}, fmt.Errorf("invalid export start invocation")
	}
	requestID, err := domain.ParseRequestID(*request)
	if err != nil {
		return command.ExportStartInput{}, fmt.Errorf("invalid export request identity")
	}
	var revision domain.Revision
	if err := revision.UnmarshalText([]byte(*revisionValue)); err != nil || revision.Value() == 0 {
		return command.ExportStartInput{}, fmt.Errorf("invalid Sequence revision")
	}
	input := command.ExportStartInput{
		RequestID: requestID, SequenceRevision: revision, Preset: *preset,
	}
	if _, err := input.ApplicationInput(); err != nil {
		return command.ExportStartInput{}, fmt.Errorf("invalid export start invocation")
	}
	return input, nil
}

func parseExportJobInvocation(set *flag.FlagSet, args []string) (domain.WorkJobID, error) {
	job := set.String("job-id", "", "durable export lineage identity")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return domain.WorkJobID{}, fmt.Errorf("invalid export job invocation")
	}
	jobID, err := domain.ParseWorkJobID(*job)
	if err != nil {
		return domain.WorkJobID{}, fmt.Errorf("invalid export job identity")
	}
	return jobID, nil
}

func parseExportCancelInvocation(set *flag.FlagSet, args []string) (command.ExportCancelInput, error) {
	request := set.String("request-id", "", "idempotent cancellation request identity")
	job := set.String("job-id", "", "durable export lineage identity")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return command.ExportCancelInput{}, fmt.Errorf("invalid export cancel invocation")
	}
	requestID, requestErr := domain.ParseRequestID(*request)
	jobID, jobErr := domain.ParseWorkJobID(*job)
	if requestErr != nil || jobErr != nil {
		return command.ExportCancelInput{}, fmt.Errorf("invalid export cancel identity")
	}
	return command.ExportCancelInput{RequestID: requestID, JobID: jobID}, nil
}
