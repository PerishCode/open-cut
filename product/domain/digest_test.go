package domain

import (
	"bytes"
	"testing"
)

func TestProjectGenesisCanonicalExpandsDefaultsAndPreservesUnicode(t *testing.T) {
	canonical, err := ProjectGenesisCanonical("A<\u2028B", DefaultSequenceFormat())
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"domain":"open-cut/project-genesis","payload":{"format":{"audioLayout":"stereo","audioSampleRate":48000,"canvasHeight":1080,"canvasWidth":1920,"colorPolicy":"sdr-rec709","frameRate":{"scale":1,"value":"30"},"pixelAspect":{"scale":1,"value":"1"}},"name":"A< B"},"schema":"open-cut/project-genesis/v1"}`)
	if !bytes.Equal(canonical, want) {
		t.Fatalf("canonical = %s\nwant      = %s", canonical, want)
	}
	digest, err := ProjectGenesisDigest("A<\u2028B", DefaultSequenceFormat())
	if err != nil {
		t.Fatal(err)
	}
	if digest.String() != "sha256:9e96bb365f3e7c6132dc4c541096b1477197b5b6d1029da5a12593c796924c56" {
		t.Fatalf("digest = %s", digest)
	}
}

func TestProjectGenesisDigestChangesWithAuthoredTextAndFormat(t *testing.T) {
	first, _ := ProjectGenesisDigest("Alpha", DefaultSequenceFormat())
	second, _ := ProjectGenesisDigest("alpha", DefaultSequenceFormat())
	if first == second {
		t.Fatal("case-distinct authored text produced the same digest")
	}
	format := DefaultSequenceFormat()
	format.CanvasWidth = 1080
	third, _ := ProjectGenesisDigest("Alpha", format)
	if first == third {
		t.Fatal("expanded format did not affect the digest")
	}
}
