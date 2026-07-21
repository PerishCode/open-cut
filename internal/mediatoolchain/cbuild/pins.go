package cbuild

// The pinned C sources live with the code that compiles them, so a pin change
// is a change to this group by construction rather than by a rule someone has
// to remember.

const (
	FFmpegSourceVersion = "8.1.2"
	FFmpegSourceURL     = "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz"
	FFmpegSignatureURL  = "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz.asc"
	FFmpegSourceSHA256  = "sha256:32faba5ef67340d54724941eae1425580791195312a4fd13bf6f820a2818bf22"
	LibVPXSourceVersion = "1.16.0"
	LibVPXSourceURL     = "https://github.com/webmproject/libvpx/archive/v1.16.0/libvpx-1.16.0.tar.gz"
	LibVPXSourceSHA256  = "sha256:7a479a3c66b9f5d5542a4c6a1b7d3768a983b1e5c14c60a9396edc9b649e015c"
	OpusSourceVersion   = "1.6.1"
	OpusSourceURL       = "https://downloads.xiph.org/releases/opus/opus-1.6.1.tar.gz"
	OpusSourceSHA256    = "sha256:6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1"
)

// SourceRecords is the pinned C source set this group compiles. The manifest
// that describes the published closure reuses it verbatim rather than
// restating pins the build does not read.
func SourceRecords() []SourceRecord {
	return []SourceRecord{
		{
			ID: "ffmpeg", Version: FFmpegSourceVersion, URL: FFmpegSourceURL,
			SignatureURL: FFmpegSignatureURL, SHA256: FFmpegSourceSHA256,
			License: "LGPL-2.1-or-later",
		},
		{
			ID: "libvpx", Version: LibVPXSourceVersion, URL: LibVPXSourceURL,
			SHA256: LibVPXSourceSHA256, License: "BSD-3-Clause",
		},
		{
			ID: "libopus", Version: OpusSourceVersion, URL: OpusSourceURL,
			SHA256: OpusSourceSHA256, License: "BSD-3-Clause",
		},
	}
}
