package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
)

func localAgentBridge(
	ctx context.Context,
	dataDir string,
	profile lifecycle.Profile,
	bridges *application.AgentBridges,
	repository application.AgentBridgeRepository,
) (*service.AgentBridgeService, string, error) {
	probe, err := service.NewAgentProbeEngine(profile)
	if err != nil {
		return nil, "incompatible", err
	}
	process, err := service.NewAgentProcessEngine(profile)
	if err != nil {
		return nil, "incompatible", err
	}
	var adapter service.AgentTurnAdapter
	state := "ready"
	stableCLI, resolveErr := service.FindStableOpenCutCLI()
	if resolveErr == nil {
		config, locateErr := service.LocateCodexCLI(ctx, service.CodexLocatorConfig{
			DataDir: dataDir, StableCLIExecutable: stableCLI,
			Candidates: service.SystemCodexCandidates(), Environment: os.Environ(),
		}, probe)
		if locateErr == nil {
			adapter, err = service.NewCodexCLIAdapter(config, process)
			if err != nil {
				return nil, "incompatible", err
			}
		} else {
			adapter = service.NewUnavailableAgentAdapter(locateErr)
			state = agentAdapterState(locateErr)
		}
	} else {
		adapter = service.NewUnavailableAgentAdapter(service.ErrAgentAdapterIncompatible)
		state = "incompatible"
	}
	hub := service.NewAgentPresentationHub()
	bridge, err := service.NewAgentBridgeService(
		ctx, bridges, repository, adapter, hub, application.ClockFunc(time.Now),
	)
	if err != nil {
		return nil, state, err
	}
	return bridge, state, nil
}

func agentAdapterState(err error) string {
	switch {
	case errors.Is(err, service.ErrAgentAdapterMissing):
		return "missing"
	case errors.Is(err, service.ErrAgentAdapterUnauthenticated):
		return "unauthenticated"
	default:
		return "incompatible"
	}
}
