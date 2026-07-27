package controlcli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/internal/devsuite"
	"github.com/PerishCode/open-cut/internal/workspace"
)

func devSuiteTarget(repository, baseDir string) (string, string, error) {
	repositoryRoot, err := filepath.Abs(repository)
	if err != nil {
		return "", "", err
	}
	selectedBaseDir, err := devsession.ResolveBaseDir(repositoryRoot, baseDir)
	if err != nil {
		return "", "", err
	}
	return repositoryRoot, selectedBaseDir, nil
}

// requireRecordedSuite refuses lifecycle commands aimed at a base directory
// that has never hosted a development suite; that almost always means the
// target was resolved against the wrong repository or a mistyped path, and
// acting there would silently create or address a different cell.
func requireRecordedSuite(baseDir string) error {
	paths, err := devsession.ResolveCellPaths(baseDir)
	if err != nil {
		return err
	}
	_, rosterErr := devsuite.LoadRoster(devsuite.RosterPath(paths.Runtime))
	if rosterErr == nil {
		return nil
	}
	if !errors.Is(rosterErr, devsuite.ErrNoRoster) {
		return rosterErr
	}
	if devsuite.LastGeneration(paths.Runtime) > 0 {
		return nil
	}
	return fmt.Errorf(
		"no development suite has ever run at %s; use `oc-control dev start`, and check --repo/--base-dir resolution",
		baseDir,
	)
}

func newDevStartCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "start", Short: "Build the workspace and start the detached development suite", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory; defaults below the repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		repositoryRoot, selectedBaseDir, err := devSuiteTarget(*repository, *baseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		result, err := devsuite.Start(cmd.Context(), repositoryRoot, selectedBaseDir, stdout, stderr)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newDevStopCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "stop", Short: "Stop the recorded development suite", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory; defaults below the repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		_, selectedBaseDir, err := devSuiteTarget(*repository, *baseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		if err := requireRecordedSuite(selectedBaseDir); err != nil {
			return asExit(fail(stderr, err))
		}
		report, err := devsuite.Stop(selectedBaseDir)
		if err != nil {
			if len(report.Members) > 0 {
				_ = writeOutput(stdout, stderr, report)
			}
			return asExit(fail(stderr, err))
		}
		return asExit(writeOutput(stdout, stderr, report))
	}
	return command
}

func newDevStatusCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "status", Short: "Report the development suite's recorded and live state", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory; defaults below the repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		_, selectedBaseDir, err := devSuiteTarget(*repository, *baseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		report, err := devsuite.Status(cmd.Context(), selectedBaseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		return asExit(writeOutput(stdout, stderr, report))
	}
	return command
}

func newDevRestartCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "restart", Short: "Stop the suite and start the next generation", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory; defaults below the repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		repositoryRoot, selectedBaseDir, err := devSuiteTarget(*repository, *baseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		if err := requireRecordedSuite(selectedBaseDir); err != nil {
			return asExit(fail(stderr, err))
		}
		// Validate the repository before stopping anything: a restart whose
		// --repo resolution is wrong must fail before it can leave the suite
		// half-stopped with no generation to start.
		if _, err := workspace.Load(repositoryRoot); err != nil {
			return asExit(fail(stderr, fmt.Errorf("%w; check --repo resolution before restarting", err)))
		}
		if _, err := devsuite.Stop(selectedBaseDir); err != nil && !errors.Is(err, devsuite.ErrNoRoster) {
			return asExit(fail(stderr, err))
		}
		result, err := devsuite.Start(cmd.Context(), repositoryRoot, selectedBaseDir, stdout, stderr)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		return asExit(writeOutput(stdout, stderr, result))
	}
	return command
}

func newDevLogsCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "logs", Short: "Snapshot the tail of suite member logs", Args: cobra.NoArgs}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory; defaults below the repository")
	app := command.Flags().String("app", "", "only this member (control or an app name)")
	lines := command.Flags().Int("lines", 40, "lines per member")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		_, selectedBaseDir, err := devSuiteTarget(*repository, *baseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		if err := requireRecordedSuite(selectedBaseDir); err != nil {
			return asExit(fail(stderr, err))
		}
		if err := devsuite.Logs(selectedBaseDir, *app, *lines, stdout); err != nil {
			return asExit(fail(stderr, err))
		}
		return nil
	}
	return command
}

func newDevControlMemberCommand(stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "control-member", Hidden: true, Args: cobra.NoArgs,
		Short: "Internal: host the cell broker and development signer as a suite member",
	}
	repository := command.Flags().String("repo", ".", "open-cut repository root")
	baseDir := command.Flags().String("base-dir", "", "development base directory")
	generation := command.Flags().Uint64("generation", 0, "suite generation")
	_ = command.Flags().String("oc-cell-stamp", "", "inert membership stamp")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		repositoryRoot, selectedBaseDir, err := devSuiteTarget(*repository, *baseDir)
		if err != nil {
			return asExit(fail(stderr, err))
		}
		if *generation == 0 {
			return asExit(fail(stderr, fmt.Errorf("control-member requires --generation")))
		}
		if err := devsuite.RunControlMember(cmd.Context(), repositoryRoot, selectedBaseDir, *generation, stderr); err != nil {
			return asExit(fail(stderr, err))
		}
		return nil
	}
	return command
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "dev: %v\n", err)
	return 1
}
