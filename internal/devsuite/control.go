package devsuite

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/internal/peerinventory"
	"github.com/PerishCode/open-cut/internal/workspace"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/broker"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

// SignerSocketPath is the fixed development signer rendezvous inside a cell
// runtime directory; app spawns receive it through the signer environment.
func SignerSocketPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "signer.sock")
}

// ConductorTokenPath persists the runtime-role token the control member mints
// for the non-resident conductor: only the runtime role may delegate sidecar
// capabilities, and the conductor needs that power for one bounded start.
func ConductorTokenPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "conductor.token")
}

// RunControlMember hosts the cell broker and the development lifecycle signer
// until a stop signal arrives, then closes both. It is the suite's control
// member: spawned detached and stamped exactly like the apps, holding the
// broker flock as the cell reservation for its own lifetime.
func RunControlMember(ctx context.Context, repositoryRoot, baseDir string, generation uint64, stderr io.Writer) error {
	paths, err := devsession.ResolveCellPaths(baseDir)
	if err != nil {
		return err
	}
	identity, err := cell.New("dev", "default")
	if err != nil {
		return err
	}
	controlConfig, err := workspace.Load(repositoryRoot)
	if err != nil {
		return err
	}
	commandRoot := filepath.Dir(filepath.Dir(baseDir))
	installation, err := lifecycle.EnsureDevelopmentInstallationIdentity(
		filepath.Join(commandRoot, "identity"), controlConfig.InstallationKeyRoles,
	)
	if err != nil {
		return fmt.Errorf("load development installation identity: %w", err)
	}
	cellBroker, err := broker.Start(broker.Options{Identity: identity, Paths: paths, Generation: generation})
	if err != nil {
		return err
	}
	defer cellBroker.Close()
	conductorToken, err := cellBroker.MintRuntimeToken("dev-conductor", 7*24*time.Hour)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(ConductorTokenPath(paths.Runtime), []byte(conductorToken+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(ConductorTokenPath(paths.Runtime))
	// The flock is held: recorded residues belong to dead sessions. Reap the
	// legacy runtime-host inventory and any prior-generation suite members.
	peerinventory.Sweep(peerinventory.Path(paths.Runtime), stderr)
	sweepRosterResidue(paths.Runtime, generation, stderr)
	signer, err := lifecycle.StartDevelopmentSigner(SignerSocketPath(paths.Runtime), installation)
	if err != nil {
		return fmt.Errorf("start development lifecycle signer: %w", err)
	}
	defer signer.Close()

	stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Fprintf(stderr, "dev control member ready: generation %d\n", generation)
	<-stop.Done()
	return nil
}

// sweepRosterResidue terminates provably-ours members recorded by an OLDER
// generation whose control member died without teardown, then drops that
// roster. The conductor writes the current generation's roster concurrently
// with this member's startup; equal or newer generations are never touched.
func sweepRosterResidue(runtimeDir string, generation uint64, stderr io.Writer) {
	roster, err := LoadRoster(RosterPath(runtimeDir))
	if err != nil {
		return
	}
	if roster.Stamp.Generation >= generation {
		return
	}
	for _, member := range roster.Members {
		if member.App == ControlApp {
			continue
		}
		switch VerifyMember(member) {
		case MemberRunning:
			fmt.Fprintf(stderr, "reaping stale suite %s member pid %d\n", member.App, member.PID)
			TerminateMember(member)
		case MemberForeign, MemberUnproven:
			fmt.Fprintf(stderr, "pid %d no longer matches recorded suite %s member; leaving it alone\n", member.PID, member.App)
		}
	}
	_ = RemoveRoster(RosterPath(runtimeDir))
}
