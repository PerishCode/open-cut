#ifndef OPEN_CUT_RENDER_NATIVE_ABI_H
#define OPEN_CUT_RENDER_NATIVE_ABI_H

#include <stddef.h>
#include <stdint.h>

enum {
  OC_TEXT_OK = 0,
  OC_TEXT_INVALID = -1,
  OC_TEXT_ALLOC = -2,
  OC_TEXT_LIBRARY = -3,
  OC_TEXT_CAPACITY = -4,
  OC_TEXT_UNSUPPORTED = -5
};

typedef struct {
  uint32_t units_per_em;
  int32_t ascender;
  int32_t descender;
} oc_text_face_metrics;

typedef struct {
  uint32_t first_cluster;
  uint32_t after_cluster;
  uint8_t level;
  uint8_t reserved[3];
} oc_text_bidi_run;

typedef struct {
  uint32_t glyph_id;
  int32_t x_advance_26_6;
  int32_t x_offset_26_6;
  int32_t y_offset_26_6;
} oc_text_shape_glyph;

typedef struct {
  uint32_t glyph_id;
  int32_t font_26_6;
  int32_t outline_26_6;
  int32_t origin_x_26_6;
  int32_t baseline_y_26_6;
} oc_text_glyph_request;

typedef struct {
  int32_t x;
  int32_t y;
  uint32_t width;
  uint32_t height;
} oc_text_glyph_bounds;

int oc_text_get_face_metrics(
    const uint8_t *font_data,
    size_t font_size,
    uint32_t face_index,
    oc_text_face_metrics *output);

int oc_text_bidi_runs(
    const uint32_t *codepoints,
    uint32_t codepoint_count,
    const uint32_t *cluster_starts,
    uint32_t cluster_count,
    oc_text_bidi_run *runs,
    uint32_t run_capacity,
    uint32_t *run_count);

int oc_text_probe_clusters(
    const uint8_t *font_data,
    size_t font_size,
    uint32_t face_index,
    const char *language,
    uint8_t direction,
    const uint8_t *text,
    uint32_t text_size,
    const uint32_t *offsets,
    uint32_t cluster_count,
    uint8_t *coverage);

int oc_text_shape(
    const uint8_t *font_data,
    size_t font_size,
    uint32_t face_index,
    const char *language,
    uint8_t direction,
    int32_t font_26_6,
    const uint8_t *text,
    uint32_t text_size,
    oc_text_shape_glyph *glyphs,
    uint32_t glyph_capacity,
    uint32_t *glyph_count);

int oc_text_glyph_bounds_many(
    const uint8_t *font_data,
    size_t font_size,
    uint32_t face_index,
    const oc_text_glyph_request *requests,
    uint32_t request_count,
    oc_text_glyph_bounds *bounds);

int oc_text_raster_glyphs(
    const uint8_t *font_data,
    size_t font_size,
    uint32_t face_index,
    const oc_text_glyph_request *requests,
    const oc_text_glyph_bounds *targets,
    uint32_t request_count,
    uint8_t *fill,
    uint8_t *outline,
    const uint32_t *offsets,
    uint32_t byte_size);

#endif
