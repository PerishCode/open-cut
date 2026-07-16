package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ProjectGenesisDigestDomain      = "open-cut/project-genesis"
	ProjectGenesisSchema            = "open-cut/project-genesis/v1"
	ProjectGenesisProposalSchema    = "open-cut/edit-proposal/project-genesis/v1"
	ProjectGenesisTransactionSchema = "open-cut/edit-transaction/project-genesis/v1"
	ProjectGenesisInverseSchema     = "open-cut/inverse-operations/project-genesis/v1"
)

var ErrInvalidDigest = errors.New("invalid sha256 digest")

type Digest string

func ParseDigest(value string) (Digest, error) {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return "", ErrInvalidDigest
	}
	for _, current := range value[7:] {
		if !strings.ContainsRune("0123456789abcdef", current) {
			return "", ErrInvalidDigest
		}
	}
	return Digest(value), nil
}

func (digest Digest) String() string {
	return string(digest)
}

func (digest Digest) MarshalJSON() ([]byte, error) {
	if _, err := ParseDigest(digest.String()); err != nil {
		return nil, err
	}
	return json.Marshal(digest.String())
}

// ProjectGenesisCanonical returns RFC 8785 bytes for the closed genesis input
// schema. The encoder is deliberately typed: it cannot canonicalize arbitrary
// caller JSON or admit duplicate keys and imprecise numbers.
func ProjectGenesisCanonical(name string, format SequenceFormat) ([]byte, error) {
	if err := validateProjectName(name); err != nil {
		return nil, err
	}
	if err := format.Validate(); err != nil {
		return nil, err
	}
	buffer := make([]byte, 0, len(name)+384)
	buffer = append(buffer, `{"domain":`...)
	buffer = appendJCSString(buffer, ProjectGenesisDigestDomain)
	buffer = append(buffer, `,"payload":{"format":`...)
	buffer = appendCanonicalSequenceFormat(buffer, format)
	buffer = append(buffer, `,"name":`...)
	buffer = appendJCSString(buffer, name)
	buffer = append(buffer, `},"schema":`...)
	buffer = appendJCSString(buffer, ProjectGenesisSchema)
	buffer = append(buffer, '}')
	return buffer, nil
}

func ProjectGenesisDigest(name string, format SequenceFormat) (Digest, error) {
	canonical, err := ProjectGenesisCanonical(name, format)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}

func ProjectGenesisProposalCanonical(genesis ProjectGenesis) ([]byte, error) {
	project := genesis.Project
	if len(project.NarrativeDocuments) != 1 || len(project.Sequences) != 1 || len(project.Sequences[0].Tracks) != 3 {
		return nil, ErrInvalidGenesisIDs
	}
	document := project.NarrativeDocuments[0]
	sequence := project.Sequences[0]
	tracks := make(map[TrackType]Track, 3)
	for _, track := range sequence.Tracks {
		tracks[track.Type] = track
	}
	video, videoOK := tracks[TrackVideo]
	audio, audioOK := tracks[TrackAudio]
	caption, captionOK := tracks[TrackCaption]
	if !videoOK || !audioOK || !captionOK {
		return nil, ErrInvalidGenesisIDs
	}
	buffer := make([]byte, 0, len(project.Name)+1024)
	buffer = append(buffer, `{"domain":"open-cut/edit-proposal","payload":{"allocation":{`...)
	buffer = append(buffer, `"audioTrack":`...)
	buffer = appendJCSString(buffer, audio.ID.String())
	buffer = append(buffer, `,"captionTrack":`...)
	buffer = appendJCSString(buffer, caption.ID.String())
	buffer = append(buffer, `,"mainSequence":`...)
	buffer = appendJCSString(buffer, sequence.ID.String())
	buffer = append(buffer, `,"narrativeDocument":`...)
	buffer = appendJCSString(buffer, document.ID.String())
	buffer = append(buffer, `,"project":`...)
	buffer = appendJCSString(buffer, project.ID.String())
	buffer = append(buffer, `,"rootSection":`...)
	buffer = appendJCSString(buffer, document.RootNodeID.String())
	buffer = append(buffer, `,"videoTrack":`...)
	buffer = appendJCSString(buffer, video.ID.String())
	buffer = append(buffer, `},"baseProjectRevision":"0","intent":"create-project","operations":[{"format":`...)
	buffer = appendCanonicalSequenceFormat(buffer, sequence.Format)
	buffer = append(buffer, `,"name":`...)
	buffer = appendJCSString(buffer, project.Name)
	buffer = append(buffer, `,"type":"create-project-genesis"}],"preconditions":[]},"schema":`...)
	buffer = appendJCSString(buffer, ProjectGenesisProposalSchema)
	buffer = append(buffer, '}')
	return buffer, nil
}

func ProjectGenesisProposalDigest(genesis ProjectGenesis) (Digest, error) {
	canonical, err := ProjectGenesisProposalCanonical(genesis)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return Digest("sha256:" + hex.EncodeToString(digest[:])), nil
}

func ProjectGenesisInverseCanonical(projectID ProjectID) ([]byte, error) {
	if projectID.IsZero() {
		return nil, ErrInvalidDurableID
	}
	buffer := []byte(`{"domain":"open-cut/inverse-operations","payload":{"operations":[{"projectId":`)
	buffer = appendJCSString(buffer, projectID.String())
	buffer = append(buffer, `,"type":"tombstone-project"}]},"schema":`...)
	buffer = appendJCSString(buffer, ProjectGenesisInverseSchema)
	buffer = append(buffer, '}')
	return buffer, nil
}

func appendCanonicalRational(buffer []byte, value RationalTime) []byte {
	buffer = append(buffer, `{"scale":`...)
	buffer = strconv.AppendInt(buffer, int64(value.Scale), 10)
	buffer = append(buffer, `,"value":`...)
	buffer = appendJCSString(buffer, value.Value.String())
	return append(buffer, '}')
}

func appendCanonicalSequenceFormat(buffer []byte, format SequenceFormat) []byte {
	buffer = append(buffer, `{"audioLayout":`...)
	buffer = appendJCSString(buffer, string(format.AudioLayout))
	buffer = append(buffer, `,"audioSampleRate":`...)
	buffer = strconv.AppendUint(buffer, uint64(format.AudioSampleRate), 10)
	buffer = append(buffer, `,"canvasHeight":`...)
	buffer = strconv.AppendUint(buffer, uint64(format.CanvasHeight), 10)
	buffer = append(buffer, `,"canvasWidth":`...)
	buffer = strconv.AppendUint(buffer, uint64(format.CanvasWidth), 10)
	buffer = append(buffer, `,"colorPolicy":`...)
	buffer = appendJCSString(buffer, string(format.ColorPolicy))
	buffer = append(buffer, `,"frameRate":`...)
	buffer = appendCanonicalRational(buffer, format.FrameRate)
	buffer = append(buffer, `,"pixelAspect":`...)
	buffer = appendCanonicalRational(buffer, format.PixelAspect)
	return append(buffer, '}')
}

func appendJCSString(buffer []byte, value string) []byte {
	if !utf8.ValidString(value) {
		panic("appendJCSString called with invalid UTF-8")
	}
	buffer = append(buffer, '"')
	for _, current := range value {
		switch current {
		case '\b':
			buffer = append(buffer, `\b`...)
		case '\t':
			buffer = append(buffer, `\t`...)
		case '\n':
			buffer = append(buffer, `\n`...)
		case '\f':
			buffer = append(buffer, `\f`...)
		case '\r':
			buffer = append(buffer, `\r`...)
		case '"':
			buffer = append(buffer, `\"`...)
		case '\\':
			buffer = append(buffer, `\\`...)
		default:
			if current < 0x20 {
				const hexadecimal = "0123456789abcdef"
				buffer = append(buffer, '\\', 'u', '0', '0', hexadecimal[current>>4], hexadecimal[current&0x0f])
			} else {
				buffer = utf8.AppendRune(buffer, current)
			}
		}
	}
	return append(buffer, '"')
}
