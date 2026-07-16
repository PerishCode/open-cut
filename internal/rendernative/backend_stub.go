//go:build !open_cut_renderer_native || !cgo

package rendernative

import "github.com/PerishCode/open-cut/internal/renderengine"

func Available() bool { return false }

func New(string, renderengine.CaptionFontBundle) (renderengine.CaptionNativeText, error) {
	return nil, ErrUnavailable
}
