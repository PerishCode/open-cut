package mediaclosure

import "testing"

func TestResourceDigestIsCanonicalAndOrderSensitive(t *testing.T) {
	resource := Resource{
		ID: "font", Kind: "font-bundle", Version: "v1", Root: "media/fonts/font",
		Files: []File{{Path: "font.ttf", SHA256: "sha256:a", ByteSize: 1}},
	}
	first, err := ResourceDigest(resource)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResourceDigest(resource)
	if err != nil || first != second || len(first) != 71 {
		t.Fatalf("first=%q second=%q err=%v", first, second, err)
	}
	resource.Files = append(resource.Files, File{Path: "bundle.json", SHA256: "sha256:b", ByteSize: 2})
	changed, err := ResourceDigest(resource)
	if err != nil || changed == first {
		t.Fatalf("changed=%q err=%v", changed, err)
	}
}
