//go:build open_cut_renderer_native && cgo

package rendernative

/*
#cgo CFLAGS: -std=c11
#cgo LDFLAGS: -lharfbuzz -lfribidi -lfreetype
#cgo darwin LDFLAGS: -lc++ -lm
#cgo linux LDFLAGS: -lstdc++ -lm -lpthread
#cgo windows LDFLAGS: -lstdc++ -lm
#include <stdlib.h>
#include "abi.h"
*/
import "C"

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/domain"
)

type backend struct {
	bundle renderengine.CaptionFontBundle
	files  map[string][]byte
	faces  map[string]renderengine.CaptionFontFace
}

func Available() bool { return true }

func New(root string, bundle renderengine.CaptionFontBundle) (renderengine.CaptionNativeText, error) {
	if bundle.Validate() != nil {
		return nil, fmt.Errorf("native caption font bundle is invalid")
	}
	cleaned := filepath.Clean(root)
	physical, err := filepath.EvalSymlinks(cleaned)
	if err != nil || physical != cleaned || !filepath.IsAbs(cleaned) {
		return nil, fmt.Errorf("native caption font root is invalid")
	}
	result := &backend{
		bundle: bundle, files: make(map[string][]byte, len(bundle.Files)),
		faces: make(map[string]renderengine.CaptionFontFace, len(bundle.Faces)),
	}
	for _, file := range bundle.Files {
		path := filepath.Join(cleaned, filepath.FromSlash(file.Path))
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
			uint64(info.Size()) != file.ByteSize {
			return nil, fmt.Errorf("native caption font file is invalid")
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || uint64(len(data)) != file.ByteSize {
			return nil, fmt.Errorf("native caption font file is unavailable")
		}
		digest := sha256.Sum256(data)
		if domain.Digest(fmt.Sprintf("sha256:%x", digest)) != file.SHA256 {
			return nil, fmt.Errorf("native caption font digest is invalid")
		}
		result.files[file.Path] = data
	}
	for _, face := range bundle.Faces {
		result.faces[face.ID] = face
	}
	return result, nil
}

func (implementation *backend) FaceMetrics(
	face renderengine.CaptionFontFace,
) (renderengine.CaptionFaceMetrics, error) {
	loaded, data, err := implementation.face(face)
	if err != nil {
		return renderengine.CaptionFaceMetrics{}, err
	}
	var output C.oc_text_face_metrics
	status := C.oc_text_get_face_metrics(
		bytePointer(data), C.size_t(len(data)), C.uint32_t(loaded.FaceIndex), &output,
	)
	runtime.KeepAlive(data)
	if err := nativeStatus(status, "face metrics"); err != nil {
		return renderengine.CaptionFaceMetrics{}, err
	}
	return renderengine.CaptionFaceMetrics{
		UnitsPerEM: uint32(output.units_per_em), Ascender: int32(output.ascender),
		Descender: int32(output.descender),
	}, nil
}

func (implementation *backend) BidiRuns(
	text string,
	clusters []renderengine.CaptionTextCluster,
) ([]renderengine.CaptionBidiRun, error) {
	if text == "" || len(clusters) == 0 || len(clusters) > math.MaxUint32 || !utf8.ValidString(text) {
		return nil, fmt.Errorf("native caption bidi input is invalid")
	}
	codepoints := []rune(text)
	if len(codepoints) == 0 || len(codepoints) > math.MaxUint32 {
		return nil, fmt.Errorf("native caption bidi input is invalid")
	}
	starts := make([]C.uint32_t, len(clusters)+1)
	byteOffset, runeOffset := 0, 0
	for index, cluster := range clusters {
		if int(cluster.ByteStart) != byteOffset || int(cluster.ByteEnd) > len(text) ||
			text[cluster.ByteStart:cluster.ByteEnd] != cluster.Text {
			return nil, fmt.Errorf("native caption bidi clusters are invalid")
		}
		starts[index] = C.uint32_t(runeOffset)
		byteOffset = int(cluster.ByteEnd)
		runeOffset += utf8.RuneCountInString(cluster.Text)
	}
	if byteOffset != len(text) || runeOffset != len(codepoints) {
		return nil, fmt.Errorf("native caption bidi cluster closure is invalid")
	}
	starts[len(clusters)] = C.uint32_t(runeOffset)
	nativeCodepoints := make([]C.uint32_t, len(codepoints))
	for index, value := range codepoints {
		nativeCodepoints[index] = C.uint32_t(value)
	}
	nativeRuns := make([]C.oc_text_bidi_run, len(clusters))
	var count C.uint32_t
	status := C.oc_text_bidi_runs(
		&nativeCodepoints[0], C.uint32_t(len(nativeCodepoints)), &starts[0],
		C.uint32_t(len(clusters)), &nativeRuns[0], C.uint32_t(len(nativeRuns)), &count,
	)
	runtime.KeepAlive(nativeCodepoints)
	runtime.KeepAlive(starts)
	if err := nativeStatus(status, "bidi"); err != nil || uint64(count) > uint64(len(nativeRuns)) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("native caption bidi output exceeds its bound")
	}
	result := make([]renderengine.CaptionBidiRun, int(count))
	for index := range result {
		result[index] = renderengine.CaptionBidiRun{
			FirstCluster: uint32(nativeRuns[index].first_cluster),
			AfterCluster: uint32(nativeRuns[index].after_cluster), Level: uint8(nativeRuns[index].level),
		}
	}
	return result, nil
}

func (implementation *backend) ProbeClusters(
	face renderengine.CaptionFontFace,
	language domain.CaptionLanguage,
	direction renderengine.CaptionDirection,
	texts []string,
) ([]renderengine.CaptionClusterCoverage, error) {
	loaded, data, err := implementation.face(face)
	if err != nil || language.Validate() != nil || len(texts) == 0 || len(texts) > math.MaxUint32 {
		return nil, fmt.Errorf("native caption cluster probe input is invalid")
	}
	flat, offsets, err := flattenCaptionText(texts)
	if err != nil {
		return nil, err
	}
	coverage := make([]uint8, len(texts))
	languageString := C.CString(language.String())
	defer C.free(unsafe.Pointer(languageString))
	status := C.oc_text_probe_clusters(
		bytePointer(data), C.size_t(len(data)), C.uint32_t(loaded.FaceIndex), languageString,
		C.uint8_t(direction), bytePointer(flat), C.uint32_t(len(flat)), &offsets[0],
		C.uint32_t(len(texts)), (*C.uint8_t)(unsafe.Pointer(&coverage[0])),
	)
	runtime.KeepAlive(data)
	runtime.KeepAlive(flat)
	runtime.KeepAlive(offsets)
	if err := nativeStatus(status, "cluster probe"); err != nil {
		return nil, err
	}
	result := make([]renderengine.CaptionClusterCoverage, len(coverage))
	for index, value := range coverage {
		result[index] = renderengine.CaptionClusterCoverage(value)
	}
	return result, nil
}

func (implementation *backend) Shape(
	requests []renderengine.CaptionShapeRequest,
) ([][]renderengine.CaptionShapeGlyph, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	result := make([][]renderengine.CaptionShapeGlyph, len(requests))
	for requestIndex, request := range requests {
		loaded, data, err := implementation.face(request.Face)
		maximum := len([]rune(request.Text))*renderengine.MaximumCaptionGlyphExpansion + 16
		if err != nil || request.Language.Validate() != nil || request.Text == "" ||
			request.Font26Dot6 <= 0 || maximum <= 0 || maximum > math.MaxUint32 {
			return nil, fmt.Errorf("native caption shape input is invalid")
		}
		glyphs := make([]C.oc_text_shape_glyph, maximum)
		var count C.uint32_t
		languageString := C.CString(request.Language.String())
		status := C.oc_text_shape(
			bytePointer(data), C.size_t(len(data)), C.uint32_t(loaded.FaceIndex), languageString,
			C.uint8_t(request.Direction), C.int32_t(request.Font26Dot6), bytePointer([]byte(request.Text)),
			C.uint32_t(len(request.Text)), &glyphs[0], C.uint32_t(len(glyphs)), &count,
		)
		C.free(unsafe.Pointer(languageString))
		runtime.KeepAlive(data)
		if err := nativeStatus(status, "shape"); err != nil || uint64(count) > uint64(len(glyphs)) {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("native caption shape output exceeds its bound")
		}
		result[requestIndex] = make([]renderengine.CaptionShapeGlyph, int(count))
		for glyphIndex := range result[requestIndex] {
			glyph := glyphs[glyphIndex]
			result[requestIndex][glyphIndex] = renderengine.CaptionShapeGlyph{
				GlyphID: uint32(glyph.glyph_id), XAdvance26Dot6: int32(glyph.x_advance_26_6),
				XOffset26Dot6: int32(glyph.x_offset_26_6), YOffset26Dot6: int32(glyph.y_offset_26_6),
			}
		}
	}
	return result, nil
}

func (implementation *backend) GlyphBounds(
	requests []renderengine.CaptionGlyphRequest,
) ([]renderengine.CaptionGlyphBounds, error) {
	result := make([]renderengine.CaptionGlyphBounds, len(requests))
	groups, err := implementation.glyphGroups(requests)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		data := implementation.files[group.face.File]
		nativeRequests := make([]C.oc_text_glyph_request, len(group.indices))
		nativeBounds := make([]C.oc_text_glyph_bounds, len(group.indices))
		for local, index := range group.indices {
			nativeRequests[local] = nativeGlyphRequest(requests[index])
		}
		status := C.oc_text_glyph_bounds_many(
			bytePointer(data), C.size_t(len(data)), C.uint32_t(group.face.FaceIndex),
			&nativeRequests[0], C.uint32_t(len(nativeRequests)), &nativeBounds[0],
		)
		runtime.KeepAlive(data)
		if err := nativeStatus(status, "glyph bounds"); err != nil {
			return nil, err
		}
		for local, index := range group.indices {
			result[index] = renderengine.CaptionGlyphBounds{
				X: int32(nativeBounds[local].x), Y: int32(nativeBounds[local].y),
				Width: uint32(nativeBounds[local].width), Height: uint32(nativeBounds[local].height),
			}
		}
	}
	return result, nil
}

func (implementation *backend) RasterGlyphs(
	requests []renderengine.CaptionGlyphRequest,
	targets []renderengine.CaptionGlyphBounds,
	fill [][]byte,
	outline [][]byte,
) error {
	if len(requests) == 0 || len(targets) != len(requests) || len(fill) != len(requests) ||
		len(outline) != len(requests) {
		return fmt.Errorf("native caption glyph raster input is invalid")
	}
	loaded, data, err := implementation.face(requests[0].Face)
	if err != nil {
		return err
	}
	nativeRequests := make([]C.oc_text_glyph_request, len(requests))
	nativeTargets := make([]C.oc_text_glyph_bounds, len(requests))
	offsets := make([]C.uint32_t, len(requests)+1)
	var byteSize uint64
	hasOutline := requests[0].Outline26Dot6 > 0
	for index, request := range requests {
		if !reflect.DeepEqual(request.Face, loaded) || (request.Outline26Dot6 > 0) != hasOutline {
			return fmt.Errorf("native caption glyph raster batch is invalid")
		}
		area := uint64(targets[index].Width) * uint64(targets[index].Height)
		if area == 0 || area != uint64(len(fill[index])) ||
			(hasOutline && area != uint64(len(outline[index]))) || (!hasOutline && len(outline[index]) != 0) ||
			math.MaxUint32-byteSize < area {
			return fmt.Errorf("native caption glyph raster buffer is invalid")
		}
		offsets[index] = C.uint32_t(byteSize)
		byteSize += area
		nativeRequests[index] = nativeGlyphRequest(request)
		nativeTargets[index] = C.oc_text_glyph_bounds{
			x: C.int32_t(targets[index].X), y: C.int32_t(targets[index].Y),
			width: C.uint32_t(targets[index].Width), height: C.uint32_t(targets[index].Height),
		}
	}
	offsets[len(requests)] = C.uint32_t(byteSize)
	flatFill := make([]byte, int(byteSize))
	var flatOutline []byte
	if hasOutline {
		flatOutline = make([]byte, int(byteSize))
	}
	status := C.oc_text_raster_glyphs(
		bytePointer(data), C.size_t(len(data)), C.uint32_t(loaded.FaceIndex), &nativeRequests[0],
		&nativeTargets[0], C.uint32_t(len(requests)), bytePointer(flatFill), bytePointer(flatOutline),
		&offsets[0], C.uint32_t(byteSize),
	)
	runtime.KeepAlive(data)
	if err := nativeStatus(status, "glyph raster"); err != nil {
		return err
	}
	for index := range requests {
		first, after := int(offsets[index]), int(offsets[index+1])
		copy(fill[index], flatFill[first:after])
		if hasOutline {
			copy(outline[index], flatOutline[first:after])
		}
	}
	return nil
}

type glyphGroup struct {
	face    renderengine.CaptionFontFace
	indices []int
}

func (implementation *backend) glyphGroups(
	requests []renderengine.CaptionGlyphRequest,
) ([]glyphGroup, error) {
	if len(requests) == 0 {
		return []glyphGroup{}, nil
	}
	groups := make([]glyphGroup, 0)
	groupByFace := make(map[string]int)
	for index, request := range requests {
		face, _, err := implementation.face(request.Face)
		if err != nil || request.GlyphID == 0 || request.Font26Dot6 <= 0 || request.Outline26Dot6 < 0 {
			return nil, fmt.Errorf("native caption glyph input is invalid")
		}
		groupIndex, exists := groupByFace[face.ID]
		if !exists {
			groupIndex = len(groups)
			groupByFace[face.ID] = groupIndex
			groups = append(groups, glyphGroup{face: face})
		}
		groups[groupIndex].indices = append(groups[groupIndex].indices, index)
	}
	return groups, nil
}

func (implementation *backend) face(
	face renderengine.CaptionFontFace,
) (renderengine.CaptionFontFace, []byte, error) {
	expected, exists := implementation.faces[face.ID]
	if !exists || !reflect.DeepEqual(expected, face) {
		return renderengine.CaptionFontFace{}, nil, fmt.Errorf("native caption face is invalid")
	}
	data, exists := implementation.files[expected.File]
	if !exists || len(data) == 0 {
		return renderengine.CaptionFontFace{}, nil, fmt.Errorf("native caption font data is unavailable")
	}
	return expected, data, nil
}

func flattenCaptionText(texts []string) ([]byte, []C.uint32_t, error) {
	offsets := make([]C.uint32_t, len(texts)+1)
	var size uint64
	for index, text := range texts {
		if text == "" || !utf8.ValidString(text) || math.MaxUint32-size < uint64(len(text)) {
			return nil, nil, fmt.Errorf("native caption text batch is invalid")
		}
		offsets[index] = C.uint32_t(size)
		size += uint64(len(text))
	}
	offsets[len(texts)] = C.uint32_t(size)
	flat := make([]byte, 0, int(size))
	for _, text := range texts {
		flat = append(flat, text...)
	}
	return flat, offsets, nil
}

func nativeGlyphRequest(request renderengine.CaptionGlyphRequest) C.oc_text_glyph_request {
	return C.oc_text_glyph_request{
		glyph_id: C.uint32_t(request.GlyphID), font_26_6: C.int32_t(request.Font26Dot6),
		outline_26_6:    C.int32_t(request.Outline26Dot6),
		origin_x_26_6:   C.int32_t(request.OriginX26Dot6),
		baseline_y_26_6: C.int32_t(request.BaselineY26Dot6),
	}
}

func bytePointer(value []byte) *C.uint8_t {
	if len(value) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&value[0]))
}

func nativeStatus(status C.int, operation string) error {
	if status == C.OC_TEXT_OK {
		return nil
	}
	return fmt.Errorf("native caption %s failed with status %d", operation, int(status))
}

var _ renderengine.CaptionNativeText = (*backend)(nil)

// Keep the build identity visible in the tagged binary and reject accidental
// link inputs that supply a different native contract.
const ABIIdentity = "open-cut-caption-native-abi-v1"

func init() {
	if strings.TrimSpace(ABIIdentity) != ABIIdentity {
		panic("native caption ABI identity is invalid")
	}
}
