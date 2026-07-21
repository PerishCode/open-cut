package cbuild

import (
	"slices"
	"strings"
)

func ValidLGPLConfiguration(configuration []string) bool {
	if len(configuration) == 0 || len(configuration) > 256 ||
		!slices.Contains(configuration, "--disable-gpl") ||
		!slices.Contains(configuration, "--disable-nonfree") ||
		!slices.Contains(configuration, "--disable-version3") ||
		!slices.Contains(configuration, "--disable-network") ||
		!slices.Contains(configuration, "--disable-protocols") ||
		!slices.Contains(configuration, "--enable-protocol=file,pipe,fd") ||
		!slices.Contains(configuration, "--disable-demuxer=hls,concat,image2") ||
		!slices.Contains(configuration, "--enable-libvpx") ||
		!slices.Contains(configuration, "--enable-libopus") ||
		!slices.Contains(configuration, "--pkg-config-flags=--static") ||
		!slices.Contains(configuration, "--enable-encoder=rawvideo,pcm_s16le,ffv1,libvpx_vp9,libopus") ||
		!slices.Contains(configuration, "--enable-muxer=rawvideo,pcm_s16le,wav,webm,matroska") ||
		!slices.Contains(configuration, "--enable-filter=select,scale,format,transpose,setsar,setparams,setpts,asetpts,aresample,colorspace,pan,aformat") ||
		!slices.Contains(configuration, "--enable-swresample") {
		return false
	}
	for _, value := range configuration {
		lower := strings.ToLower(value)
		if value == "" || len(value) > 1024 || lower == "--enable-gpl" || lower == "--enable-nonfree" ||
			strings.Contains(lower, "libx264") || strings.Contains(lower, "libx265") {
			return false
		}
	}
	return true
}
