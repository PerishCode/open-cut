package businessacceptance

import (
	"context"
	"fmt"
	"path/filepath"
)

type CreatorToCLIOptions struct {
	CDPEndpoint            string
	ProjectName            string
	FixturePath            string
	ExpectedAudioChannels  string
	ExpectedVideo          bool
	RunIntent              string
	AuthoredText           string
	AcquireProductionModel bool
	DeliveryPath           string
	NativeSaveDialog       NativeSaveDialog
	CLI                    CommandExecutor
}

func RunCreatorToCLI(ctx context.Context, options CreatorToCLIOptions) (Observation, error) {
	if options.ProjectName == "" || options.FixturePath == "" || options.ExpectedAudioChannels == "" ||
		options.RunIntent == "" ||
		options.AuthoredText == "" || options.CLI == nil {
		return Observation{}, fmt.Errorf("Creator-to-CLI acceptance options are incomplete")
	}
	if (options.DeliveryPath == "") != (options.NativeSaveDialog == nil) {
		return Observation{}, fmt.Errorf("Creator-to-CLI delivery options are incomplete")
	}
	cdp, err := ConnectCreatorCDP(ctx, options.CDPEndpoint)
	if err != nil {
		return Observation{}, err
	}
	defer cdp.Close()
	creator := Creator{CDP: cdp}
	if err := creator.Bootstrap(ctx, options.ProjectName, options.FixturePath); err != nil {
		return Observation{}, err
	}
	actor := Actor{CLI: options.CLI}
	pairing, err := actor.DiscoverAndRequestPairing(ctx)
	if err != nil {
		return Observation{}, err
	}
	if pairing.ID == "" {
		return Observation{}, fmt.Errorf("business actor did not receive a pairing identity")
	}
	if err := creator.ApprovePairing(ctx); err != nil {
		return Observation{}, err
	}
	observation, err := actor.ObserveCreatorBootstrap(ctx, options.ProjectName, filepath.Base(options.FixturePath))
	if err != nil {
		return Observation{}, err
	}
	run, err := actor.BeginAndObserveStandaloneRun(ctx, observation.ProjectID, options.RunIntent)
	if err != nil {
		return Observation{}, err
	}
	observation.RunID = run.RunID
	observation.TurnID = run.TurnID
	observation.RunStatus = run.RunStatus
	observation, err = actor.ObserveMediaPipeline(
		ctx, observation, options.ExpectedAudioChannels, options.ExpectedVideo,
	)
	if err != nil {
		return Observation{}, err
	}
	if options.AcquireProductionModel {
		if err := creator.AcquireTranscriptionModel(ctx); err != nil {
			return Observation{}, err
		}
		observation, err = actor.ObserveProductionTranscript(ctx, observation)
		if err != nil {
			return Observation{}, err
		}
	}
	observation, err = actor.ProposeAndApplyAuthoredText(ctx, observation, options.AuthoredText)
	if err != nil || !options.AcquireProductionModel {
		return observation, err
	}
	observation, err = actor.ProposeAndApplyTranscriptSourceExcerpt(ctx, observation)
	if err != nil {
		return Observation{}, err
	}
	observation, err = actor.DeriveAndApplyRoughCut(ctx, observation)
	if err != nil {
		return Observation{}, err
	}
	observation, err = actor.InspectCommittedSequenceFrames(ctx, observation)
	if err != nil {
		return Observation{}, err
	}
	observation, err = actor.ExportCommittedSequence(ctx, observation)
	if err != nil || options.NativeSaveDialog == nil {
		return observation, err
	}
	if err := creator.SaveAndRevealExport(ctx, options.DeliveryPath, options.NativeSaveDialog); err != nil {
		return Observation{}, err
	}
	if err := verifyDeliveredExport(options.DeliveryPath, observation); err != nil {
		return Observation{}, err
	}
	observation.ExportDeliveryStatus = "saved-revealed"
	observation.ExportDeliveryName = filepath.Base(options.DeliveryPath)
	return observation, nil
}
