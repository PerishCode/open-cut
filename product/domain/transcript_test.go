package domain

import "testing"

func TestTranscriptArtifactRequiresExactBindingAndLexicalEvidence(t *testing.T) {
	artifact := transcriptFixture(t)
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	changed := artifact
	changed.Segments = append([]TranscriptSegment(nil), artifact.Segments...)
	changed.Segments[0].Text = "different"
	if changed.Validate() == nil {
		t.Fatal("token concatenation drift was accepted")
	}
	changed = artifact
	changed.Binding.ModelContentDigest = Digest("sha256:" + repeatHex("b"))
	if changed.Validate() == nil {
		t.Fatal("binding digest drift was accepted")
	}
}

func TestTranscriptArtifactAllowsAudioWithNoRecognizedSpeech(t *testing.T) {
	artifact := transcriptFixture(t)
	artifact.Segments = []TranscriptSegment{}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
}

func transcriptFixture(t *testing.T) TranscriptArtifact {
	t.Helper()
	binding := TranscriptBinding{
		Schema:                 TranscriptBindingSchema,
		AssetID:                mustTranscriptID(t, ParseAssetID, "018f0000-0000-7000-8000-000000000001"),
		Fingerprint:            Digest("sha256:" + repeatHex("1")),
		SourceStreamID:         mustTranscriptID(t, ParseSourceStreamID, "018f0000-0000-7000-8000-000000000002"),
		SourceDescriptorDigest: Digest("sha256:" + repeatHex("2")),
		SelectionPolicy:        TranscriptSelectionDefaultV1, NormalizationPolicy: TranscriptNormalizationV1,
		LanguagePolicy: TranscriptLanguageAutoOriginal, EngineVersion: "whisper.cpp@test",
		EngineTarget:    "mac-arm64",
		ModelResourceID: mustTranscriptID(t, ParseResourceID, "018f0000-0000-7000-8000-000000000003"),
		ModelName:       "whisper-small-multilingual-v1", ModelVersion: "small-v3",
		ModelEntryDigest:   Digest("sha256:" + repeatHex("3")),
		ModelContentDigest: Digest("sha256:" + repeatHex("4")),
	}
	_, bindingDigest, err := CanonicalDigest("open-cut/transcript-binding", TranscriptBindingSchema, binding)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := NewRationalTime(1, 1)
	duration, _ := NewRationalTime(1, 2)
	count, _ := NewUInt64(32_000)
	bytes, _ := NewUInt64(64_000)
	confidence := uint16(9_000)
	return TranscriptArtifact{
		Schema:    TranscriptArtifactSchema,
		ID:        mustTranscriptID(t, ParseArtifactID, "018f0000-0000-7000-8000-000000000004"),
		ProjectID: mustTranscriptID(t, ParseProjectID, "018f0000-0000-7000-8000-000000000005"),
		Binding:   binding, BindingDigest: bindingDigest, DetectedLanguage: "en",
		LanguageConfidenceBasisPoints: &confidence,
		Normalization: TranscriptNormalizationProof{
			SourceStartTime: start, SampleRate: TranscriptSampleRate, Channels: 1, SampleFormat: "s16le",
			SampleCount: count, PCMByteSize: bytes, PCMDigest: Digest("sha256:" + repeatHex("5")),
			ChannelPolicy: "stereo-equal-v1", TimingPolicy: "audio-frame-pts-gap-fill-v1",
		},
		Segments: []TranscriptSegment{{
			ID:      mustTranscriptID(t, ParseTranscriptSegmentID, "018f0000-0000-7000-8000-000000000006"),
			Ordinal: 0, SourceRange: TimeRange{Start: start, Duration: duration}, Text: "hello",
			Tokens: []TranscriptToken{{
				ID:      mustTranscriptID(t, ParseTranscriptTokenID, "018f0000-0000-7000-8000-000000000007"),
				Ordinal: 0, SourceRange: TimeRange{Start: start, Duration: duration}, Text: "hello",
				ConfidenceBasisPoints: &confidence,
			}},
		}},
	}
}

func mustTranscriptID[Kind any](t *testing.T, parse func(string) (ID[Kind], error), value string) ID[Kind] {
	t.Helper()
	id, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repeatHex(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
}
