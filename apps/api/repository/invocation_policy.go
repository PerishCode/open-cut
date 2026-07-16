package repository

import (
	"context"
	"fmt"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) LoadCLIInvocationPolicy(
	ctx context.Context,
) (application.InvocationPolicySettings, error) {
	var revisionValue uint64
	var output string
	var waitMilliseconds uint32
	if err := repository.db.QueryRowContext(ctx, `
SELECT revision, output_mode, wait_milliseconds
FROM cli_invocation_settings
WHERE singleton = 1`).Scan(&revisionValue, &output, &waitMilliseconds); err != nil {
		return application.InvocationPolicySettings{}, fmt.Errorf("load CLI invocation policy: %w", err)
	}
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return application.InvocationPolicySettings{}, err
	}
	settings := application.InvocationPolicySettings{
		Revision: revision,
		Policy: application.InvocationPolicy{
			Output: application.OutputMode(output), WaitMilliseconds: waitMilliseconds,
		},
	}
	if err := settings.Validate(); err != nil {
		return application.InvocationPolicySettings{}, err
	}
	return settings, nil
}

func (repository *MemoryProjects) LoadCLIInvocationPolicy(
	_ context.Context,
) (application.InvocationPolicySettings, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return repository.invocationPolicy, nil
}
