package application

import (
	"fmt"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

type EditTrackState struct {
	ID         domain.TrackID
	SequenceID domain.SequenceID
	Revision   domain.Revision
	Type       domain.TrackType
}

type EditSourceStreamState struct {
	ID            domain.SourceStreamID
	AssetID       domain.AssetID
	AssetRevision domain.Revision
	Descriptor    domain.SourceStreamDescriptor
}

type EditTranscriptSegmentState struct {
	ID          domain.TranscriptSegmentID
	Ordinal     uint32
	SourceRange domain.TimeRange
	Text        string
	Tokens      []domain.TranscriptToken
}

type EditTranscriptArtifactState struct {
	ID             domain.ArtifactID
	AssetID        domain.AssetID
	Fingerprint    domain.Digest
	SourceStreamID domain.SourceStreamID
	Language       domain.CaptionLanguage
	Segments       map[string]EditTranscriptSegmentState
}

type EditNormalizationState struct {
	ProjectID             domain.ProjectID
	ProjectRevision       domain.Revision
	DocumentID            domain.NarrativeDocumentID
	DocumentRevision      domain.Revision
	SequenceID            domain.SequenceID
	SequenceRevision      domain.Revision
	Sections              map[string]domain.NarrativeSectionState
	Tracks                map[string]EditTrackState
	AuthoredTexts         map[string]domain.AuthoredTextState
	SourceExcerpts        map[string]domain.SourceExcerptState
	VisualIntents         map[string]domain.VisualIntentState
	Notes                 map[string]domain.NoteState
	SectionChildCounts    map[string]int
	SourceExcerptEvidence map[string]domain.SourceExcerptEvidenceStatus
	TranscriptCorrections map[string]domain.TranscriptCorrectionState
	TranscriptArtifacts   map[string]EditTranscriptArtifactState
	Captions              map[string]domain.CaptionState
	Clips                 map[string]domain.ClipState
	LinkGroups            map[string]domain.LinkGroupState
	LinkGroupClips        map[string][]domain.ClipID
	SourceStreams         map[string]EditSourceStreamState
	Alignments            map[string]domain.AlignmentState
	NodeAlignments        map[string][]domain.AlignmentID
	CaptionAlignments     map[string][]domain.AlignmentID
	ClipAlignments        map[string][]domain.AlignmentID
}

type NormalizeEditInput struct {
	ProposalID domain.ProposalID
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	RunID      domain.RunID
	TurnID     domain.TurnID
	Actor      domain.ActorRef
	Allocation []domain.LocalAllocation
	Input      EditProposeInput
	CreatedAt  time.Time
	State      EditNormalizationState
}

func NormalizeEditProposal(input NormalizeEditInput) (domain.EditProposal, []byte, error) {
	if err := validateNormalizationInput(input); err != nil {
		return domain.EditProposal{}, nil, err
	}
	normalizer, err := newEditNormalizer(input)
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	if err := normalizer.normalizeEntityOperations(); err != nil {
		return domain.EditProposal{}, nil, err
	}
	if err := normalizer.validateLinkGroups(); err != nil {
		return domain.EditProposal{}, nil, err
	}
	if err := normalizer.validateCaptionOverlaps(); err != nil {
		return domain.EditProposal{}, nil, err
	}
	if err := normalizer.validateClipOverlaps(); err != nil {
		return domain.EditProposal{}, nil, err
	}
	if err := normalizer.normalizeAlignmentOperations(); err != nil {
		return domain.EditProposal{}, nil, err
	}
	if len(normalizer.operations) > 512 || len(normalizer.inverse) > 512 {
		return domain.EditProposal{}, nil, ErrEditInvalid
	}
	if err := normalizer.validateAlignmentEffects(); err != nil {
		return domain.EditProposal{}, nil, err
	}
	normalizer.appendAggregateChanges()
	var runID *domain.RunID
	var turnID *domain.TurnID
	if !input.RunID.IsZero() {
		value := input.RunID
		runID = &value
	}
	if !input.TurnID.IsZero() {
		value := input.TurnID
		turnID = &value
	}
	proposal := domain.EditProposal{
		ID: input.ProposalID, ProjectID: input.ProjectID, SequenceID: &input.SequenceID,
		RunID: runID, TurnID: turnID,
		RequestID: input.Input.RequestID, Actor: input.Actor, Intent: input.Input.Intent,
		BaseProjectRevision: input.Input.BaseProjectRevision,
		Preconditions:       append([]domain.EntityPrecondition(nil), input.Input.Preconditions...),
		Allocation:          append([]domain.LocalAllocation(nil), input.Allocation...),
		Operations:          normalizer.operations, InversePreview: normalizer.inverse,
		Changes: normalizer.changes,
		Impact:  domain.EditImpact{Classifier: domain.EditImpactClassifierV1, Class: "reversible-local"},
		Status:  domain.ProposalOpen, CreatedAt: input.CreatedAt.UTC(),
	}
	sort.Slice(proposal.Preconditions, func(left, right int) bool {
		if proposal.Preconditions[left].Kind != proposal.Preconditions[right].Kind {
			return proposal.Preconditions[left].Kind < proposal.Preconditions[right].Kind
		}
		return proposal.Preconditions[left].ID < proposal.Preconditions[right].ID
	})
	canonical, digest, err := domain.CanonicalDigest("open-cut/edit-proposal", domain.EditProposalSchema, struct {
		Actor               domain.ActorRef                  `json:"actor"`
		Allocation          []domain.LocalAllocation         `json:"allocation"`
		BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
		Changes             []domain.EntityRevisionChange    `json:"changes"`
		Impact              domain.EditImpact                `json:"impact"`
		Intent              string                           `json:"intent"`
		Inverse             []domain.NormalizedEditOperation `json:"inverse"`
		Operations          []domain.NormalizedEditOperation `json:"operations"`
		Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
		ProjectID           domain.ProjectID                 `json:"projectId"`
		RunID               *domain.RunID                    `json:"runId,omitempty"`
		SequenceID          domain.SequenceID                `json:"sequenceId"`
		TurnID              *domain.TurnID                   `json:"turnId,omitempty"`
	}{
		Actor: proposal.Actor, Allocation: proposal.Allocation,
		BaseProjectRevision: proposal.BaseProjectRevision, Changes: proposal.Changes,
		Impact: proposal.Impact, Intent: proposal.Intent, Inverse: proposal.InversePreview,
		Operations: proposal.Operations, Preconditions: proposal.Preconditions,
		ProjectID: proposal.ProjectID, RunID: proposal.RunID, SequenceID: *proposal.SequenceID, TurnID: proposal.TurnID,
	})
	if err != nil {
		return domain.EditProposal{}, nil, err
	}
	proposal.Digest = digest
	return proposal, canonical, nil
}

type editNormalizer struct {
	input            NormalizeEditInput
	allocations      map[string]domain.LocalAllocation
	conditions       map[string]domain.Revision
	sections         map[string]domain.NarrativeSectionState
	texts            map[string]domain.AuthoredTextState
	sourceExcerpts   map[string]domain.SourceExcerptState
	visualIntents    map[string]domain.VisualIntentState
	notes            map[string]domain.NoteState
	sectionChildren  map[string]int
	corrections      map[string]domain.TranscriptCorrectionState
	captions         map[string]domain.CaptionState
	clips            map[string]domain.ClipState
	linkGroups       map[string]domain.LinkGroupState
	linkGroupClips   map[string][]domain.ClipID
	alignments       map[string]domain.AlignmentState
	touched          map[string]struct{}
	alignmentEffects map[string]domain.AlignmentStatus
	operations       []domain.NormalizedEditOperation
	inverse          []domain.NormalizedEditOperation
	changes          []domain.EntityRevisionChange
	narrativeChanged bool
	sequenceChanged  bool
	parentChanges    map[string]domain.Revision
	trackChanges     map[string]domain.Revision
}

func newEditNormalizer(input NormalizeEditInput) (*editNormalizer, error) {
	allocations := make(map[string]domain.LocalAllocation, len(input.Allocation))
	for _, allocation := range input.Allocation {
		if _, duplicate := allocations[allocation.Local.String()]; duplicate ||
			validateEntityID(allocation.Kind, allocation.ID) != nil {
			return nil, ErrEditInvalid
		}
		allocations[allocation.Local.String()] = allocation
	}
	conditions := make(map[string]domain.Revision, len(input.Input.Preconditions))
	for _, condition := range input.Input.Preconditions {
		conditions[entityKey(condition.Kind, condition.ID)] = condition.Revision
	}
	return &editNormalizer{
		input: input, allocations: allocations, conditions: conditions,
		sections: cloneNarrativeSections(input.State.Sections),
		texts:    cloneAuthoredTexts(input.State.AuthoredTexts), captions: cloneCaptions(input.State.Captions),
		sourceExcerpts: cloneSourceExcerpts(input.State.SourceExcerpts),
		visualIntents:  cloneVisualIntents(input.State.VisualIntents), notes: cloneNotes(input.State.Notes),
		sectionChildren: cloneIntMap(input.State.SectionChildCounts),
		corrections:     cloneTranscriptCorrections(input.State.TranscriptCorrections),
		clips:           cloneClips(input.State.Clips), linkGroups: cloneLinkGroups(input.State.LinkGroups),
		linkGroupClips: cloneLinkGroupClips(input.State.LinkGroupClips),
		alignments:     cloneAlignments(input.State.Alignments), touched: make(map[string]struct{}),
		alignmentEffects: make(map[string]domain.AlignmentStatus),
		parentChanges:    make(map[string]domain.Revision), trackChanges: make(map[string]domain.Revision),
		operations: make([]domain.NormalizedEditOperation, 0, len(input.Input.Operations)),
		inverse:    make([]domain.NormalizedEditOperation, 0, len(input.Input.Operations)),
	}, nil
}

func (normalizer *editNormalizer) normalizeEntityOperations() error {
	for _, operation := range normalizer.input.Input.Operations {
		switch operation.Type {
		case domain.EditInsertSection, domain.EditInsertAuthoredText, domain.EditInsertVisualIntent,
			domain.EditInsertNote:
			if err := normalizer.insertNarrativeNode(operation); err != nil {
				return err
			}
		case domain.EditUpdateSection, domain.EditUpdateAuthoredText, domain.EditUpdateVisualIntent,
			domain.EditUpdateNote:
			if err := normalizer.updateNarrativeNode(operation); err != nil {
				return err
			}
		case domain.EditMoveNarrativeNode:
			if err := normalizer.moveNarrativeNode(operation); err != nil {
				return err
			}
		case domain.EditRemoveNarrativeNode:
			if err := normalizer.removeNarrativeNode(operation); err != nil {
				return err
			}
		case domain.EditAddCaption:
			if err := normalizer.addCaption(operation); err != nil {
				return err
			}
		case domain.EditUpdateCaption:
			if err := normalizer.updateCaption(operation, false); err != nil {
				return err
			}
		case domain.EditRemoveCaption:
			if err := normalizer.updateCaption(operation, true); err != nil {
				return err
			}
		case domain.EditAddClip:
			if err := normalizer.addClip(operation); err != nil {
				return err
			}
		case domain.EditMoveClip:
			if err := normalizer.moveClip(operation); err != nil {
				return err
			}
		case domain.EditTrimClip:
			if err := normalizer.trimClip(operation); err != nil {
				return err
			}
		case domain.EditSplitClip:
			if err := normalizer.splitClip(operation); err != nil {
				return err
			}
		case domain.EditRemoveClip:
			if err := normalizer.removeClip(operation); err != nil {
				return err
			}
		case domain.EditLinkClips:
			if err := normalizer.linkClips(operation); err != nil {
				return err
			}
		case domain.EditUnlinkClips:
			if err := normalizer.unlinkClips(operation); err != nil {
				return err
			}
		case domain.EditAddTranscriptCorrection:
			if err := normalizer.addTranscriptCorrection(operation); err != nil {
				return err
			}
		case domain.EditUpdateTranscriptCorrection:
			if err := normalizer.updateTranscriptCorrection(operation, false); err != nil {
				return err
			}
		case domain.EditRemoveTranscriptCorrection:
			if err := normalizer.updateTranscriptCorrection(operation, true); err != nil {
				return err
			}
		case domain.EditInsertSourceExcerpt:
			if err := normalizer.insertSourceExcerpt(operation); err != nil {
				return err
			}
		case domain.EditDeriveCaptions:
			if err := normalizer.deriveCaptions(operation); err != nil {
				return err
			}
		case domain.EditDeriveRoughCut:
			if err := normalizer.deriveRoughCut(operation); err != nil {
				return err
			}
		}
	}
	return nil
}

func (normalizer *editNormalizer) normalizeAlignmentOperations() error {
	for _, operation := range normalizer.input.Input.Operations {
		switch operation.Type {
		case domain.EditBindAlignment:
			if err := normalizer.bindAlignment(operation); err != nil {
				return err
			}
		case domain.EditRemapAlignment:
			if err := normalizer.remapAlignment(operation); err != nil {
				return err
			}
		case domain.EditMarkAlignmentStale:
			if err := normalizer.updateAlignment(*operation.AlignmentID, domain.AlignmentStale); err != nil {
				return err
			}
		case domain.EditUnbindAlignment:
			if err := normalizer.updateAlignment(*operation.AlignmentID, domain.AlignmentUnbound); err != nil {
				return err
			}
		}
	}
	return nil
}

func (normalizer *editNormalizer) addCaption(operation EditOperationInput) error {
	allocation := normalizer.allocations[operation.CreateAs.String()]
	if allocation.Kind != domain.EntityCaption {
		return ErrEditInvalid
	}
	id, _ := domain.ParseCaptionID(allocation.ID)
	if _, exists := normalizer.input.State.Captions[id.String()]; exists || normalizer.markTouched(domain.EntityCaption, id.String()) != nil {
		return ErrEditInvalid
	}
	track := normalizer.input.State.Tracks[operation.TrackID.String()]
	if track.ID.IsZero() || track.SequenceID != normalizer.input.SequenceID || track.Type != domain.TrackCaption {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
		return err
	}
	revision, _ := domain.NewRevision(1)
	state := domain.CaptionState{
		ID: id, Revision: revision, SequenceID: track.SequenceID, TrackID: track.ID,
		Range: *operation.Range, Language: *operation.Language, Text: *operation.Text,
		Provenance: domain.CaptionProvenance{Kind: domain.CaptionProvenanceManual},
	}
	normalizer.captions[id.String()] = state
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutCaption, Caption: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutCaption, Caption: &domain.CaptionState{
			ID: id, Revision: mustNext(revision), SequenceID: state.SequenceID, TrackID: state.TrackID,
			Range: state.Range, Language: state.Language, Text: state.Text,
			Provenance: state.Provenance, Tombstoned: true,
		}},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(domain.EntityCaption, id.String(), revision, false))
	normalizer.sequenceChanged = true
	normalizer.trackChanges[track.ID.String()] = track.Revision
	return nil
}

func (normalizer *editNormalizer) updateCaption(operation EditOperationInput, remove bool) error {
	current, exists := normalizer.captions[operation.CaptionID.String()]
	if !exists || current.Tombstoned || normalizer.markTouched(domain.EntityCaption, current.ID.String()) != nil {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityCaption, current.ID.String(), current.Revision); err != nil {
		return err
	}
	track := normalizer.input.State.Tracks[current.TrackID.String()]
	if track.ID.IsZero() {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityTrack, track.ID.String(), track.Revision); err != nil {
		return err
	}
	normalizer.trackChanges[track.ID.String()] = track.Revision
	next := current
	next.Revision = mustNext(current.Revision)
	if remove {
		next.Tombstoned = true
	} else {
		next.Range = *operation.Range
		next.Language = *operation.Language
		next.Text = *operation.Text
	}
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	normalizer.captions[current.ID.String()] = next
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutCaption, Caption: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutCaption, Caption: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityCaption, current.ID.String(), current.Revision, next.Revision, remove,
	))
	normalizer.sequenceChanged = true
	return nil
}

func (normalizer *editNormalizer) bindAlignment(operation EditOperationInput) error {
	allocation := normalizer.allocations[operation.CreateAs.String()]
	if allocation.Kind != domain.EntityAlignment {
		return ErrEditInvalid
	}
	id, _ := domain.ParseAlignmentID(allocation.ID)
	if _, exists := normalizer.input.State.Alignments[id.String()]; exists || normalizer.markTouched(domain.EntityAlignment, id.String()) != nil {
		return ErrEditInvalid
	}
	nodeID, err := normalizer.resolveNodeReference(*operation.NarrativeNode)
	if err != nil {
		return err
	}
	nodeRevision, nodeOK := normalizer.narrativeNodeRevision(nodeID.String())
	if !nodeOK {
		return ErrEditInvalid
	}
	if operation.NarrativeNode.ID != "" {
		if err := normalizer.require(domain.EntityNarrativeNode, nodeID.String(), nodeRevision); err != nil {
			return err
		}
	}
	targets, err := normalizer.normalizeAlignmentTargets(operation.AlignmentTargets, false)
	if err != nil {
		return err
	}
	revision, _ := domain.NewRevision(1)
	state := domain.AlignmentState{
		ID: id, Revision: revision, NarrativeNodeID: nodeID, NarrativeNodeRevision: nodeRevision,
		SequenceID: normalizer.input.SequenceID, Targets: targets, Status: domain.AlignmentExact,
	}
	normalizer.alignments[id.String()] = state
	inverse := state
	inverse.Revision = mustNext(revision)
	inverse.Targets = cloneAlignmentTargets(state.Targets)
	inverse.Status = domain.AlignmentUnbound
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &state},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &inverse},
	)
	normalizer.changes = append(normalizer.changes, newEntityChange(domain.EntityAlignment, id.String(), revision, false))
	return nil
}

func (normalizer *editNormalizer) normalizeAlignmentTargets(
	inputs []AlignmentTargetInput,
	allowTouched bool,
) ([]domain.AlignmentTarget, error) {
	targets := make([]domain.AlignmentTarget, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case domain.AlignmentTargetCaption:
			id, err := normalizer.resolveCaptionReference(*input.Caption)
			if err != nil {
				return nil, err
			}
			caption, exists := normalizer.captions[id.String()]
			if !exists || caption.Tombstoned || caption.SequenceID != normalizer.input.SequenceID ||
				!rangeWithin(*input.LocalRange, caption.Range.Duration) {
				return nil, ErrEditInvalid
			}
			_, captionTouched := normalizer.touched[entityKey(domain.EntityCaption, id.String())]
			if input.Caption.ID != "" && (!allowTouched || !captionTouched) {
				if err := normalizer.require(domain.EntityCaption, id.String(), caption.Revision); err != nil {
					return nil, err
				}
			}
			targets = append(targets, domain.AlignmentTarget{Type: input.Type, Caption: &domain.CaptionAlignmentTarget{
				CaptionID: id, CaptionRevision: caption.Revision, LocalRange: *input.LocalRange,
			}})
		case domain.AlignmentTargetClip:
			id, err := normalizer.resolveClipReference(*input.Clip)
			if err != nil {
				return nil, err
			}
			clip, exists := normalizer.clips[id.String()]
			if !exists || clip.Tombstoned || clip.SequenceID != normalizer.input.SequenceID ||
				!rangeWithin(*input.LocalRange, clip.TimelineRange.Duration) {
				return nil, ErrEditInvalid
			}
			_, clipTouched := normalizer.touched[entityKey(domain.EntityClip, id.String())]
			if input.Clip.ID != "" && (!allowTouched || !clipTouched) {
				if err := normalizer.require(domain.EntityClip, id.String(), clip.Revision); err != nil {
					return nil, err
				}
			}
			targets = append(targets, domain.AlignmentTarget{Type: input.Type, Clip: &domain.ClipAlignmentTarget{
				ClipID: id, ClipRevision: clip.Revision, LocalRange: *input.LocalRange,
			}})
		case domain.AlignmentTargetTimeline:
			if *input.SequenceRevision != normalizer.input.State.SequenceRevision {
				return nil, ErrEditConflict
			}
			if err := normalizer.require(
				domain.EntitySequence, normalizer.input.SequenceID.String(), normalizer.input.State.SequenceRevision,
			); err != nil {
				return nil, err
			}
			targets = append(targets, domain.AlignmentTarget{Type: input.Type, Timeline: &domain.TimelineAlignmentTarget{
				SequenceRevision: *input.SequenceRevision, Range: *input.TimelineRange,
			}})
		default:
			return nil, ErrEditInvalid
		}
	}
	sort.Slice(targets, func(left, right int) bool {
		return alignmentTargetKey(targets[left]) < alignmentTargetKey(targets[right])
	})
	for index := 1; index < len(targets); index++ {
		if alignmentTargetKey(targets[index-1]) == alignmentTargetKey(targets[index]) {
			return nil, ErrEditInvalid
		}
	}
	return targets, nil
}

func alignmentTargetKey(target domain.AlignmentTarget) string {
	switch target.Type {
	case domain.AlignmentTargetCaption:
		return fmt.Sprintf("caption\x00%s\x00%s/%d\x00%s/%d", target.Caption.CaptionID,
			target.Caption.LocalRange.Start.Value, target.Caption.LocalRange.Start.Scale,
			target.Caption.LocalRange.Duration.Value, target.Caption.LocalRange.Duration.Scale)
	case domain.AlignmentTargetClip:
		return fmt.Sprintf("clip\x00%s\x00%s/%d\x00%s/%d", target.Clip.ClipID,
			target.Clip.LocalRange.Start.Value, target.Clip.LocalRange.Start.Scale,
			target.Clip.LocalRange.Duration.Value, target.Clip.LocalRange.Duration.Scale)
	case domain.AlignmentTargetTimeline:
		return fmt.Sprintf("timeline\x00%s/%d\x00%s/%d", target.Timeline.Range.Start.Value,
			target.Timeline.Range.Start.Scale, target.Timeline.Range.Duration.Value,
			target.Timeline.Range.Duration.Scale)
	default:
		return ""
	}
}

func (normalizer *editNormalizer) updateAlignment(id domain.AlignmentID, status domain.AlignmentStatus) error {
	current, exists := normalizer.alignments[id.String()]
	if !exists || normalizer.markTouched(domain.EntityAlignment, id.String()) != nil {
		return ErrEditInvalid
	}
	if err := normalizer.require(domain.EntityAlignment, current.ID.String(), current.Revision); err != nil {
		return err
	}
	next := current
	next.Revision = mustNext(current.Revision)
	next.Targets = cloneAlignmentTargets(current.Targets)
	next.Status = status
	inverse := current
	inverse.Revision = mustNext(next.Revision)
	inverse.Targets = cloneAlignmentTargets(current.Targets)
	normalizer.alignments[id.String()] = next
	normalizer.alignmentEffects[id.String()] = status
	normalizer.appendOperation(
		domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &next},
		domain.NormalizedEditOperation{Type: domain.NormalizedPutAlignment, Alignment: &inverse},
	)
	normalizer.changes = append(normalizer.changes, existingEntityChange(
		domain.EntityAlignment, current.ID.String(), current.Revision, next.Revision, false,
	))
	return nil
}

func (normalizer *editNormalizer) validateAlignmentEffects() error {
	for key := range normalizer.touched {
		kind, id := splitEntityKey(key)
		var dependent []domain.AlignmentID
		switch kind {
		case domain.EntityNarrativeNode:
			dependent = normalizer.input.State.NodeAlignments[id]
		case domain.EntityCaption:
			dependent = normalizer.input.State.CaptionAlignments[id]
		case domain.EntityClip:
			dependent = normalizer.input.State.ClipAlignments[id]
		default:
			continue
		}
		for _, alignmentID := range dependent {
			status, handled := normalizer.alignmentEffects[alignmentID.String()]
			if !handled || (status != domain.AlignmentExact && status != domain.AlignmentStale && status != domain.AlignmentUnbound) {
				return ErrEditInvalid
			}
		}
	}
	return nil
}

func (normalizer *editNormalizer) validateCaptionOverlaps() error {
	for key := range normalizer.touched {
		kind, id := splitEntityKey(key)
		if kind != domain.EntityCaption {
			continue
		}
		caption := normalizer.captions[id]
		if caption.Tombstoned {
			continue
		}
		for otherID, other := range normalizer.captions {
			if otherID == id || other.Tombstoned || other.TrackID != caption.TrackID {
				continue
			}
			if rangesOverlap(caption.Range, other.Range) {
				return ErrEditInvalid
			}
		}
	}
	return nil
}

func (normalizer *editNormalizer) appendAggregateChanges() {
	for id, before := range normalizer.parentChanges {
		if _, directlyTouched := normalizer.touched[entityKey(domain.EntityNarrativeNode, id)]; directlyTouched {
			continue
		}
		normalizer.changes = append(normalizer.changes, existingEntityChange(
			domain.EntityNarrativeNode, id, before, mustNext(before), false,
		))
	}
	for id, before := range normalizer.trackChanges {
		normalizer.changes = append(normalizer.changes, existingEntityChange(
			domain.EntityTrack, id, before, mustNext(before), false,
		))
	}
	sort.Slice(normalizer.changes, func(left, right int) bool {
		if normalizer.changes[left].Kind != normalizer.changes[right].Kind {
			return normalizer.changes[left].Kind < normalizer.changes[right].Kind
		}
		return normalizer.changes[left].ID < normalizer.changes[right].ID
	})
	for left, right := 0, len(normalizer.inverse)-1; left < right; left, right = left+1, right-1 {
		normalizer.inverse[left], normalizer.inverse[right] = normalizer.inverse[right], normalizer.inverse[left]
	}
}
