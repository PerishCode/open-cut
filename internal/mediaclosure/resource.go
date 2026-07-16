package mediaclosure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const ResourceDomain = "open-cut/media-resource-closure/v1"

type Resource struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Root    string `json:"root"`
	Files   []File `json:"files"`
}

type File struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	ByteSize uint64 `json:"byteSize"`
}

func ResourceDigest(resource Resource) (string, error) {
	encoded, err := json.Marshal(resource)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(ResourceDomain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(encoded)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}
