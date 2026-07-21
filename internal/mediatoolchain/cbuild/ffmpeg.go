package cbuild

import (
	"path/filepath"

	"github.com/PerishCode/open-cut/utils/target"
)

func buildConfiguration(
	compiler, buildRoot, dependencyRoot string,
	buildTarget target.Target,
) []string {
	linkFlags := "-L" + filepath.Join(dependencyRoot, "lib")
	if buildTarget.Platform == target.Win {
		linkFlags += " -static"
	}
	return []string{
		"--disable-gpl", "--disable-nonfree", "--disable-version3", "--disable-network",
		"--disable-protocols", "--enable-protocol=file,pipe,fd", "--disable-demuxer=hls,concat,image2",
		"--disable-autodetect", "--disable-doc", "--disable-debug", "--enable-ffmpeg", "--disable-ffplay",
		"--enable-ffprobe", "--disable-avdevice", "--enable-libvpx", "--enable-libopus",
		"--pkg-config-flags=--static",
		"--disable-encoders", "--enable-encoder=rawvideo,pcm_s16le,ffv1,libvpx_vp9,libopus",
		"--disable-muxers", "--enable-muxer=rawvideo,pcm_s16le,wav,webm,matroska", "--disable-filters",
		"--enable-filter=select,scale,format,transpose,setsar,setparams,setpts,asetpts,aresample,colorspace,pan,aformat",
		"--enable-swscale", "--enable-swresample",
		"--disable-asm", "--disable-stripping", "--cc=" + compiler,
		"--extra-cflags=-I" + filepath.Join(dependencyRoot, "include") + " -ffile-prefix-map=" + buildRoot + "=.",
		"--extra-ldflags=" + linkFlags,
	}
}
