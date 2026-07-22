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
	// Progressf, if set, is called as each step begins. The journey is a long
	// sequence under one deadline, so without it a timeout says only that the
	// whole thing did not finish. The last step announced before the deadline
	// is the one that was running when time ran out - which distinguishes a
	// genuinely slow upstream step from a downstream step that hung.
	Progressf func(format string, args ...any)
}

func (options CreatorToCLIOptions) progress(format string, args ...any) {
	if options.Progressf != nil {
		options.Progressf(format, args...)
	}
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
	options.progress("connect renderer CDP")
	cdp, err := ConnectCreatorCDP(ctx, options.CDPEndpoint)
	if err != nil {
		return Observation{}, err
	}
	defer cdp.Close()
	creator := Creator{CDP: cdp}
	options.progress("bootstrap Creator project and import footage")
	if err := creator.Bootstrap(ctx, options.ProjectName, options.FixturePath); err != nil {
		return Observation{}, err
	}
	actor := Actor{CLI: options.CLI}
	options.progress("discover and request Agent pairing")
	pairing, err := actor.DiscoverAndRequestPairing(ctx)
	if err != nil {
		return Observation{}, err
	}
	if pairing.ID == "" {
		return Observation{}, fmt.Errorf("business actor did not receive a pairing identity")
	}
	options.progress("approve pairing")
	if err := creator.ApprovePairing(ctx); err != nil {
		return Observation{}, err
	}
	options.progress("observe Creator bootstrap")
	observation, err := actor.ObserveCreatorBootstrap(ctx, options.ProjectName, filepath.Base(options.FixturePath))
	if err != nil {
		return Observation{}, err
	}
	options.progress("begin standalone run")
	run, err := actor.BeginAndObserveStandaloneRun(ctx, observation.ProjectID, options.RunIntent)
	if err != nil {
		return Observation{}, err
	}
	observation.RunID = run.RunID
	observation.TurnID = run.TurnID
	observation.RunStatus = run.RunStatus
	options.progress("observe media pipeline")
	observation, err = actor.ObserveMediaPipeline(
		ctx, observation, options.ExpectedAudioChannels, options.ExpectedVideo,
	)
	if err != nil {
		return Observation{}, err
	}
	if options.AcquireProductionModel {
		options.progress("acquire production transcription model")
		if err := creator.AcquireTranscriptionModel(ctx); err != nil {
			return Observation{}, err
		}
		options.progress("observe production transcript")
		observation, err = actor.ObserveProductionTranscript(ctx, observation)
		if err != nil {
			return Observation{}, err
		}
	}
	options.progress("propose and apply authored text")
	observation, err = actor.ProposeAndApplyAuthoredText(ctx, observation, options.AuthoredText)
	if err != nil || !options.AcquireProductionModel {
		return observation, err
	}
	options.progress("propose and apply transcript source excerpt")
	observation, err = actor.ProposeAndApplyTranscriptSourceExcerpt(ctx, observation)
	if err != nil {
		return Observation{}, err
	}
	options.progress("derive and apply rough cut")
	observation, err = actor.DeriveAndApplyRoughCut(ctx, observation)
	if err != nil {
		return Observation{}, err
	}
	options.progress("inspect committed sequence frames")
	observation, err = actor.InspectCommittedSequenceFrames(ctx, observation)
	if err != nil {
		return Observation{}, err
	}
	options.progress("export committed sequence")
	observation, err = actor.ExportCommittedSequence(ctx, observation)
	if err != nil || options.NativeSaveDialog == nil {
		return observation, err
	}
	options.progress("save and reveal export")
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
