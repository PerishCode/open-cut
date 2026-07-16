package domain

const (
	CaptionPolicyReadableV1       = "readable-captions-v1"
	CaptionBoundaryTerminalV1     = "terminal-punctuation-v1"
	CaptionTimingForwardPaddingV1 = "forward-pad-no-overlap-v1"
)

type CaptionDerivationPolicy struct {
	ID                    string       `json:"id" enum:"readable-captions-v1"`
	MaximumLines          uint8        `json:"maximumLines" minimum:"2" maximum:"2"`
	MaximumLineGraphemes  uint16       `json:"maximumLineGraphemes" minimum:"42" maximum:"42"`
	MinimumDuration       RationalTime `json:"minimumDuration"`
	MaximumDuration       RationalTime `json:"maximumDuration"`
	MaximumGap            RationalTime `json:"maximumGap"`
	MaximumReadingRate    uint16       `json:"maximumReadingRate" minimum:"20" maximum:"20"`
	BoundaryPolicy        string       `json:"boundaryPolicy" enum:"terminal-punctuation-v1"`
	TimingPolicy          string       `json:"timingPolicy" enum:"forward-pad-no-overlap-v1"`
	UnicodeSegmentationID string       `json:"unicodeSegmentationId" enum:"unicode-egc-15.0.0-uniseg-v0.4.7"`
}

func ReadableCaptionPolicyV1() CaptionDerivationPolicy {
	minimum, _ := NewRationalTime(1, 1)
	maximum, _ := NewRationalTime(6, 1)
	gap, _ := NewRationalTime(3, 4)
	return CaptionDerivationPolicy{
		ID: CaptionPolicyReadableV1, MaximumLines: 2, MaximumLineGraphemes: 42,
		MinimumDuration: minimum, MaximumDuration: maximum, MaximumGap: gap,
		MaximumReadingRate: 20, BoundaryPolicy: CaptionBoundaryTerminalV1,
		TimingPolicy:          CaptionTimingForwardPaddingV1,
		UnicodeSegmentationID: "unicode-egc-15.0.0-uniseg-v0.4.7",
	}
}

func (policy CaptionDerivationPolicy) Validate() error {
	expected := ReadableCaptionPolicyV1()
	minimum, minimumErr := policy.MinimumDuration.Compare(expected.MinimumDuration)
	maximum, maximumErr := policy.MaximumDuration.Compare(expected.MaximumDuration)
	gap, gapErr := policy.MaximumGap.Compare(expected.MaximumGap)
	if minimumErr != nil || maximumErr != nil || gapErr != nil || minimum != 0 || maximum != 0 || gap != 0 ||
		policy.ID != expected.ID || policy.MaximumLines != expected.MaximumLines ||
		policy.MaximumLineGraphemes != expected.MaximumLineGraphemes ||
		policy.MaximumReadingRate != expected.MaximumReadingRate ||
		policy.BoundaryPolicy != expected.BoundaryPolicy || policy.TimingPolicy != expected.TimingPolicy ||
		policy.UnicodeSegmentationID != expected.UnicodeSegmentationID {
		return ErrInvalidRationalTime
	}
	return nil
}

type CaptionProvenanceKind string

const (
	CaptionProvenanceManual               CaptionProvenanceKind = "manual"
	CaptionProvenanceTranscriptDerivation CaptionProvenanceKind = "transcript-derivation"
)

type CaptionDerivationProvenance struct {
	SourceExcerptID       NarrativeNodeID                   `json:"sourceExcerptId"`
	SourceExcerptRevision Revision                          `json:"sourceExcerptRevision"`
	AssetID               AssetID                           `json:"assetId"`
	AcceptedFingerprint   Digest                            `json:"acceptedFingerprint" format:"sha256-digest"`
	ArtifactID            ArtifactID                        `json:"transcriptArtifactId"`
	SourceStreamID        SourceStreamID                    `json:"sourceStreamId"`
	SegmentIDs            []TranscriptSegmentID             `json:"segmentIds" minItems:"1" maxItems:"256" nullable:"false"`
	CorrectionRevisions   []TranscriptCorrectionRevisionRef `json:"correctionRevisions" maxItems:"256" nullable:"false"`
	ClipID                ClipID                            `json:"clipId"`
	ClipRevision          Revision                          `json:"clipRevision"`
	ClipSourceRange       TimeRange                         `json:"clipSourceRange"`
	ClipTimelineRange     TimeRange                         `json:"clipTimelineRange"`
	EvidenceSourceRange   TimeRange                         `json:"evidenceSourceRange"`
	Policy                CaptionDerivationPolicy           `json:"policy"`
	DerivedRange          TimeRange                         `json:"derivedRange"`
	DerivedLanguage       CaptionLanguage                   `json:"derivedLanguage" maxLength:"64"`
	DerivedText           string                            `json:"derivedText" minLength:"1" maxLength:"262144"`
}

type CaptionProvenance struct {
	Kind       CaptionProvenanceKind        `json:"kind" enum:"manual,transcript-derivation"`
	Derivation *CaptionDerivationProvenance `json:"derivation,omitempty"`
}

type CaptionContentStatus string

const (
	CaptionContentExact    CaptionContentStatus = "exact"
	CaptionContentModified CaptionContentStatus = "modified"
)

type CaptionEvidenceStatus string

const (
	CaptionEvidenceExact CaptionEvidenceStatus = "exact"
	CaptionEvidenceStale CaptionEvidenceStatus = "stale"
)

type CaptionProvenanceStatus struct {
	Content  CaptionContentStatus  `json:"content" enum:"exact,modified"`
	Evidence CaptionEvidenceStatus `json:"evidence" enum:"exact,stale"`
}
