package application

import (
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestTranscriptAudioSelectionPrefersDefaultThenLowestIndex(t *testing.T) {
	video := transcriptSelectionStream(t, 0, domain.MediaVideo, nil)
	audioLow := transcriptSelectionStream(t, 1, domain.MediaAudio, nil)
	audioDefaultHigh := transcriptSelectionStream(t, 2, domain.MediaAudio, []string{"default"})
	audioDefaultLow := transcriptSelectionStream(t, 3, domain.MediaAudio, []string{"default"})
	audioDefaultLow.Descriptor.Index = 1
	audioLow.Descriptor.Index = 2

	selected, found, err := SelectDefaultTranscriptAudioStream([]domain.SourceStream{
		video, audioLow, audioDefaultHigh, audioDefaultLow,
	})
	if err != nil || !found || selected.ID != audioDefaultLow.ID {
		t.Fatalf("unexpected selection: selected=%+v found=%v err=%v", selected, found, err)
	}
}

func TestTranscriptAudioSelectionReportsNoAudio(t *testing.T) {
	_, found, err := SelectDefaultTranscriptAudioStream([]domain.SourceStream{
		transcriptSelectionStream(t, 0, domain.MediaVideo, nil),
	})
	if err != nil || found {
		t.Fatalf("expected typed no-audio result, found=%v err=%v", found, err)
	}
}

func transcriptSelectionStream(
	t *testing.T,
	index uint32,
	mediaType domain.MediaType,
	dispositions []string,
) domain.SourceStream {
	t.Helper()
	id, err := domain.ParseSourceStreamID("018f1f13-7b9c-7a01-8000-00000000000" + string(rune('1'+index)))
	if err != nil {
		t.Fatal(err)
	}
	timeBase, err := domain.NewRationalTime(1, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := domain.SourceStreamDescriptor{
		Index: index, MediaType: mediaType, Codec: "test", TimeBase: timeBase,
		Dispositions: dispositions,
	}
	if mediaType == domain.MediaVideo {
		descriptor.Video = &domain.VideoStreamFacts{Width: 1920, Height: 1080}
	} else {
		descriptor.Audio = &domain.AudioStreamFacts{SampleRate: 48_000, Channels: 2}
	}
	return domain.SourceStream{ID: id, Descriptor: descriptor}
}
