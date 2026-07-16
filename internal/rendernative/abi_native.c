//go:build open_cut_renderer_native

#include "abi.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

#include <ft2build.h>
#include FT_COLOR_H
#include FT_FREETYPE_H
#include FT_GLYPH_H
#include FT_STROKER_H

#include <fribidi.h>
#include <hb-ft.h>
#include <hb.h>

enum {
  OC_TEXT_DIRECTION_LTR = 1,
  OC_TEXT_DIRECTION_RTL = 2,
  OC_TEXT_COVERAGE_ABSENT = 0,
  OC_TEXT_COVERAGE_MONOCHROME = 1,
  OC_TEXT_COVERAGE_COLOR = 2
};

typedef struct {
  FT_Library library;
  FT_Face face;
  hb_font_t *font;
} oc_text_face;

typedef struct {
  uint32_t logical;
  uint32_t visual;
  uint8_t level;
} oc_visual_cluster;

static void oc_close_face(oc_text_face *context) {
  if (context == NULL) {
    return;
  }
  if (context->font != NULL) {
    hb_font_destroy(context->font);
  }
  if (context->face != NULL) {
    FT_Done_Face(context->face);
  }
  if (context->library != NULL) {
    FT_Done_FreeType(context->library);
  }
  memset(context, 0, sizeof(*context));
}

static int oc_open_face(const uint8_t *data, size_t size, uint32_t face_index,
                        int32_t font_26_6, int with_harfbuzz,
                        oc_text_face *context) {
  if (data == NULL || size == 0 || size > LONG_MAX || context == NULL ||
      font_26_6 < 0) {
    return OC_TEXT_INVALID;
  }
  memset(context, 0, sizeof(*context));
  if (FT_Init_FreeType(&context->library) != 0 ||
      FT_New_Memory_Face(context->library, data, (FT_Long)size,
                         (FT_Long)face_index, &context->face) != 0 ||
      context->face == NULL || !FT_IS_SCALABLE(context->face) ||
      context->face->units_per_EM == 0) {
    oc_close_face(context);
    return OC_TEXT_LIBRARY;
  }
  if (font_26_6 > 0 &&
      FT_Set_Char_Size(context->face, 0, (FT_F26Dot6)font_26_6, 72, 72) != 0) {
    oc_close_face(context);
    return OC_TEXT_LIBRARY;
  }
  if (with_harfbuzz) {
    context->font = hb_ft_font_create_referenced(context->face);
    if (context->font == NULL) {
      oc_close_face(context);
      return OC_TEXT_LIBRARY;
    }
    hb_ft_font_set_load_flags(context->font,
                              FT_LOAD_NO_HINTING | FT_LOAD_NO_AUTOHINT |
                                  FT_LOAD_NO_BITMAP | FT_LOAD_NO_SVG);
  }
  return OC_TEXT_OK;
}

static hb_buffer_t *oc_shape_buffer(oc_text_face *context, const uint8_t *text,
                                    uint32_t size, const char *language,
                                    uint8_t direction) {
  if (context == NULL || context->font == NULL || text == NULL || size == 0 ||
      language == NULL ||
      (direction != OC_TEXT_DIRECTION_LTR &&
       direction != OC_TEXT_DIRECTION_RTL)) {
    return NULL;
  }
  hb_buffer_t *buffer = hb_buffer_create();
  if (buffer == NULL) {
    return NULL;
  }
  hb_buffer_set_cluster_level(buffer,
                              HB_BUFFER_CLUSTER_LEVEL_MONOTONE_GRAPHEMES);
  hb_buffer_set_random_state(buffer, 0x4f435431U);
  hb_buffer_set_flags(buffer, HB_BUFFER_FLAG_REMOVE_DEFAULT_IGNORABLES |
                                  HB_BUFFER_FLAG_DO_NOT_INSERT_DOTTED_CIRCLE);
  hb_buffer_add_utf8(buffer, (const char *)text, (int)size, 0, (int)size);
  hb_buffer_set_language(buffer, hb_language_from_string(language, -1));
  hb_buffer_guess_segment_properties(buffer);
  hb_buffer_set_direction(buffer, direction == OC_TEXT_DIRECTION_RTL
                                      ? HB_DIRECTION_RTL
                                      : HB_DIRECTION_LTR);
  hb_shape(context->font, buffer, NULL, 0);
  return buffer;
}

int oc_text_get_face_metrics(const uint8_t *font_data, size_t font_size,
                             uint32_t face_index,
                             oc_text_face_metrics *output) {
  if (output == NULL) {
    return OC_TEXT_INVALID;
  }
  oc_text_face context;
  int status = oc_open_face(font_data, font_size, face_index, 0, 0, &context);
  if (status != OC_TEXT_OK) {
    return status;
  }
  output->units_per_em = context.face->units_per_EM;
  output->ascender = context.face->ascender;
  output->descender = context.face->descender;
  oc_close_face(&context);
  return OC_TEXT_OK;
}

static int oc_visual_cluster_compare(const void *left_value,
                                     const void *right_value) {
  const oc_visual_cluster *left = (const oc_visual_cluster *)left_value;
  const oc_visual_cluster *right = (const oc_visual_cluster *)right_value;
  if (left->visual < right->visual) {
    return -1;
  }
  if (left->visual > right->visual) {
    return 1;
  }
  return left->logical < right->logical ? -1 : left->logical > right->logical;
}

int oc_text_bidi_runs(const uint32_t *codepoints, uint32_t codepoint_count,
                      const uint32_t *cluster_starts, uint32_t cluster_count,
                      oc_text_bidi_run *runs, uint32_t run_capacity,
                      uint32_t *run_count) {
  if (codepoints == NULL || codepoint_count == 0 || cluster_starts == NULL ||
      cluster_count == 0 || runs == NULL || run_count == NULL ||
      run_capacity < cluster_count || cluster_starts[0] != 0 ||
      cluster_starts[cluster_count] != codepoint_count ||
      codepoint_count > INT_MAX) {
    return OC_TEXT_INVALID;
  }
  for (uint32_t index = 0; index < cluster_count; index++) {
    if (cluster_starts[index] >= cluster_starts[index + 1]) {
      return OC_TEXT_INVALID;
    }
  }
  FriBidiStrIndex *logical_to_visual =
      (FriBidiStrIndex *)calloc(codepoint_count, sizeof(FriBidiStrIndex));
  FriBidiLevel *levels =
      (FriBidiLevel *)calloc(codepoint_count, sizeof(FriBidiLevel));
  oc_visual_cluster *clusters =
      (oc_visual_cluster *)calloc(cluster_count, sizeof(oc_visual_cluster));
  if (logical_to_visual == NULL || levels == NULL || clusters == NULL) {
    free(logical_to_visual);
    free(levels);
    free(clusters);
    return OC_TEXT_ALLOC;
  }
  FriBidiParType base = FRIBIDI_PAR_ON;
  FriBidiLevel maximum = fribidi_log2vis(
      (const FriBidiChar *)codepoints, (FriBidiStrIndex)codepoint_count, &base,
      NULL, logical_to_visual, NULL, levels);
  if (maximum == 0) {
    free(logical_to_visual);
    free(levels);
    free(clusters);
    return OC_TEXT_LIBRARY;
  }
  for (uint32_t cluster = 0; cluster < cluster_count; cluster++) {
    uint32_t first = cluster_starts[cluster];
    uint32_t after = cluster_starts[cluster + 1];
    uint32_t minimum_visual = UINT32_MAX;
    uint32_t maximum_visual = 0;
    FriBidiLevel level = levels[first];
    for (uint32_t rune = first; rune < after; rune++) {
      if (logical_to_visual[rune] < 0 ||
          (uint32_t)logical_to_visual[rune] >= codepoint_count ||
          levels[rune] != level) {
        free(logical_to_visual);
        free(levels);
        free(clusters);
        return OC_TEXT_UNSUPPORTED;
      }
      uint32_t visual = (uint32_t)logical_to_visual[rune];
      if (visual < minimum_visual) {
        minimum_visual = visual;
      }
      if (visual > maximum_visual) {
        maximum_visual = visual;
      }
    }
    if (maximum_visual - minimum_visual + 1 != after - first) {
      free(logical_to_visual);
      free(levels);
      free(clusters);
      return OC_TEXT_UNSUPPORTED;
    }
    clusters[cluster].logical = cluster;
    clusters[cluster].visual = minimum_visual;
    clusters[cluster].level = level;
  }
  qsort(clusters, cluster_count, sizeof(oc_visual_cluster),
        oc_visual_cluster_compare);
  uint32_t count = 0;
  for (uint32_t first = 0; first < cluster_count;) {
    uint8_t level = clusters[first].level;
    int step = (level & 1) != 0 ? -1 : 1;
    uint32_t after = first + 1;
    while (after < cluster_count && clusters[after].level == level &&
           (int64_t)clusters[after].logical ==
               (int64_t)clusters[after - 1].logical + step) {
      after++;
    }
    uint32_t logical_first = clusters[first].logical;
    uint32_t logical_last = clusters[after - 1].logical;
    runs[count].first_cluster = logical_first < logical_last ? logical_first
                                                             : logical_last;
    runs[count].after_cluster =
        (logical_first > logical_last ? logical_first : logical_last) + 1;
    runs[count].level = level;
    memset(runs[count].reserved, 0, sizeof(runs[count].reserved));
    count++;
    first = after;
  }
  *run_count = count;
  free(logical_to_visual);
  free(levels);
  free(clusters);
  return OC_TEXT_OK;
}

static uint8_t oc_probe_buffer(oc_text_face *context, hb_buffer_t *buffer) {
  unsigned int glyph_count = 0;
  hb_glyph_info_t *glyphs = hb_buffer_get_glyph_infos(buffer, &glyph_count);
  if (glyphs == NULL || glyph_count == 0) {
    return OC_TEXT_COVERAGE_ABSENT;
  }
  uint8_t result = OC_TEXT_COVERAGE_MONOCHROME;
  for (unsigned int index = 0; index < glyph_count; index++) {
    if (glyphs[index].codepoint == 0 ||
        FT_Load_Glyph(context->face, glyphs[index].codepoint,
                      FT_LOAD_NO_HINTING | FT_LOAD_NO_AUTOHINT |
                          FT_LOAD_NO_BITMAP | FT_LOAD_NO_SVG) != 0) {
      return OC_TEXT_COVERAGE_ABSENT;
    }
    FT_LayerIterator iterator;
    FT_UInt layer_glyph = 0;
    FT_UInt color_index = 0;
    memset(&iterator, 0, sizeof(iterator));
    if (FT_HAS_COLOR(context->face) &&
        (FT_Get_Color_Glyph_Layer(context->face, glyphs[index].codepoint,
                                  &layer_glyph, &color_index, &iterator) ||
         context->face->glyph->format == FT_GLYPH_FORMAT_BITMAP ||
         context->face->glyph->format == FT_GLYPH_FORMAT_SVG)) {
      result = OC_TEXT_COVERAGE_COLOR;
    } else if (context->face->glyph->format != FT_GLYPH_FORMAT_OUTLINE) {
      return OC_TEXT_COVERAGE_ABSENT;
    }
  }
  return result;
}

int oc_text_probe_clusters(const uint8_t *font_data, size_t font_size,
                           uint32_t face_index, const char *language,
                           uint8_t direction, const uint8_t *text,
                           uint32_t text_size, const uint32_t *offsets,
                           uint32_t cluster_count, uint8_t *coverage) {
  if (language == NULL || text == NULL || text_size == 0 || offsets == NULL ||
      cluster_count == 0 || coverage == NULL || offsets[0] != 0 ||
      offsets[cluster_count] != text_size) {
    return OC_TEXT_INVALID;
  }
  oc_text_face context;
  int status = oc_open_face(font_data, font_size, face_index, 64 * 64, 1,
                            &context);
  if (status != OC_TEXT_OK) {
    return status;
  }
  for (uint32_t index = 0; index < cluster_count; index++) {
    if (offsets[index] >= offsets[index + 1]) {
      oc_close_face(&context);
      return OC_TEXT_INVALID;
    }
    hb_buffer_t *buffer =
        oc_shape_buffer(&context, text + offsets[index],
                        offsets[index + 1] - offsets[index], language,
                        direction);
    if (buffer == NULL) {
      oc_close_face(&context);
      return OC_TEXT_ALLOC;
    }
    coverage[index] = oc_probe_buffer(&context, buffer);
    hb_buffer_destroy(buffer);
  }
  oc_close_face(&context);
  return OC_TEXT_OK;
}

int oc_text_shape(const uint8_t *font_data, size_t font_size,
                  uint32_t face_index, const char *language, uint8_t direction,
                  int32_t font_26_6, const uint8_t *text, uint32_t text_size,
                  oc_text_shape_glyph *glyphs, uint32_t glyph_capacity,
                  uint32_t *glyph_count) {
  if (font_26_6 <= 0 || glyphs == NULL || glyph_capacity == 0 ||
      glyph_count == NULL) {
    return OC_TEXT_INVALID;
  }
  oc_text_face context;
  int status = oc_open_face(font_data, font_size, face_index, font_26_6, 1,
                            &context);
  if (status != OC_TEXT_OK) {
    return status;
  }
  hb_buffer_t *buffer =
      oc_shape_buffer(&context, text, text_size, language, direction);
  if (buffer == NULL) {
    oc_close_face(&context);
    return OC_TEXT_ALLOC;
  }
  unsigned int count = 0;
  hb_glyph_info_t *information = hb_buffer_get_glyph_infos(buffer, &count);
  hb_glyph_position_t *positions = hb_buffer_get_glyph_positions(buffer, &count);
  if (information == NULL || positions == NULL || count == 0) {
    hb_buffer_destroy(buffer);
    oc_close_face(&context);
    return OC_TEXT_UNSUPPORTED;
  }
  if (count > glyph_capacity) {
    hb_buffer_destroy(buffer);
    oc_close_face(&context);
    return OC_TEXT_CAPACITY;
  }
  for (unsigned int index = 0; index < count; index++) {
    if (information[index].codepoint == 0 || positions[index].y_advance != 0) {
      hb_buffer_destroy(buffer);
      oc_close_face(&context);
      return OC_TEXT_UNSUPPORTED;
    }
    glyphs[index].glyph_id = information[index].codepoint;
    glyphs[index].x_advance_26_6 = positions[index].x_advance;
    glyphs[index].x_offset_26_6 = positions[index].x_offset;
    glyphs[index].y_offset_26_6 = -positions[index].y_offset;
  }
  *glyph_count = count;
  hb_buffer_destroy(buffer);
  oc_close_face(&context);
  return OC_TEXT_OK;
}

static int64_t oc_floor_div_64(int32_t value) {
  int64_t quotient = value / 64;
  int64_t remainder = value % 64;
  if (remainder < 0) {
    quotient--;
  }
  return quotient;
}

static int oc_glyph_pair(oc_text_face *context,
                         const oc_text_glyph_request *request, FT_Glyph *fill,
                         FT_Glyph *outline, int64_t *base_x, int64_t *base_y) {
  if (context == NULL || request == NULL || fill == NULL || outline == NULL ||
      base_x == NULL || base_y == NULL || request->glyph_id == 0 ||
      request->font_26_6 <= 0 || request->outline_26_6 < 0 ||
      FT_Set_Char_Size(context->face, 0, request->font_26_6, 72, 72) != 0 ||
      FT_Load_Glyph(context->face, request->glyph_id,
                    FT_LOAD_NO_HINTING | FT_LOAD_NO_AUTOHINT |
                        FT_LOAD_NO_BITMAP | FT_LOAD_NO_SVG) != 0 ||
      context->face->glyph->format != FT_GLYPH_FORMAT_OUTLINE ||
      FT_Get_Glyph(context->face->glyph, fill) != 0) {
    return OC_TEXT_LIBRARY;
  }
  *outline = NULL;
  *base_x = oc_floor_div_64(request->origin_x_26_6);
  *base_y = oc_floor_div_64(request->baseline_y_26_6);
  FT_Vector delta;
  delta.x = request->origin_x_26_6 - *base_x * 64;
  delta.y = -(request->baseline_y_26_6 - *base_y * 64);
  if (FT_Glyph_Transform(*fill, NULL, &delta) != 0) {
    FT_Done_Glyph(*fill);
    *fill = NULL;
    return OC_TEXT_LIBRARY;
  }
  if (request->outline_26_6 > 0) {
    if (FT_Glyph_Copy(*fill, outline) != 0) {
      FT_Done_Glyph(*fill);
      *fill = NULL;
      return OC_TEXT_LIBRARY;
    }
    FT_Stroker stroker = NULL;
    if (FT_Stroker_New(context->library, &stroker) != 0) {
      FT_Done_Glyph(*outline);
      FT_Done_Glyph(*fill);
      *outline = NULL;
      *fill = NULL;
      return OC_TEXT_LIBRARY;
    }
    FT_Stroker_Set(stroker, request->outline_26_6, FT_STROKER_LINECAP_ROUND,
                   FT_STROKER_LINEJOIN_ROUND, 0);
    FT_Error error = FT_Glyph_StrokeBorder(outline, stroker, 0, 1);
    FT_Stroker_Done(stroker);
    if (error != 0) {
      FT_Done_Glyph(*outline);
      FT_Done_Glyph(*fill);
      *outline = NULL;
      *fill = NULL;
      return OC_TEXT_LIBRARY;
    }
  }
  return OC_TEXT_OK;
}

static void oc_union_cbox(FT_Glyph glyph, int64_t base_x, int64_t base_y,
                          int *has_box, int64_t *first_x, int64_t *first_y,
                          int64_t *after_x, int64_t *after_y) {
  if (glyph == NULL) {
    return;
  }
  FT_BBox box;
  FT_Glyph_Get_CBox(glyph, FT_GLYPH_BBOX_PIXELS, &box);
  if (box.xMax <= box.xMin || box.yMax <= box.yMin) {
    return;
  }
  int64_t x0 = base_x + box.xMin;
  int64_t x1 = base_x + box.xMax;
  int64_t y0 = base_y - box.yMax;
  int64_t y1 = base_y - box.yMin;
  if (!*has_box) {
    *first_x = x0;
    *first_y = y0;
    *after_x = x1;
    *after_y = y1;
    *has_box = 1;
    return;
  }
  if (x0 < *first_x) *first_x = x0;
  if (y0 < *first_y) *first_y = y0;
  if (x1 > *after_x) *after_x = x1;
  if (y1 > *after_y) *after_y = y1;
}

int oc_text_glyph_bounds_many(const uint8_t *font_data, size_t font_size,
                              uint32_t face_index,
                              const oc_text_glyph_request *requests,
                              uint32_t request_count,
                              oc_text_glyph_bounds *bounds) {
  if (requests == NULL || request_count == 0 || bounds == NULL) {
    return OC_TEXT_INVALID;
  }
  oc_text_face context;
  int status = oc_open_face(font_data, font_size, face_index, 0, 0, &context);
  if (status != OC_TEXT_OK) {
    return status;
  }
  for (uint32_t index = 0; index < request_count; index++) {
    FT_Glyph fill = NULL;
    FT_Glyph outline = NULL;
    int64_t base_x = 0;
    int64_t base_y = 0;
    status = oc_glyph_pair(&context, &requests[index], &fill, &outline,
                           &base_x, &base_y);
    if (status != OC_TEXT_OK) {
      oc_close_face(&context);
      return status;
    }
    int has_box = 0;
    int64_t first_x = 0, first_y = 0, after_x = 0, after_y = 0;
    oc_union_cbox(fill, base_x, base_y, &has_box, &first_x, &first_y,
                  &after_x, &after_y);
    oc_union_cbox(outline, base_x, base_y, &has_box, &first_x, &first_y,
                  &after_x, &after_y);
    FT_Done_Glyph(fill);
    if (outline != NULL) FT_Done_Glyph(outline);
    if (!has_box) {
      memset(&bounds[index], 0, sizeof(bounds[index]));
      continue;
    }
    if (first_x < INT32_MIN || first_y < INT32_MIN || after_x > INT32_MAX ||
        after_y > INT32_MAX || after_x <= first_x || after_y <= first_y ||
        (uint64_t)(after_x - first_x) > UINT32_MAX ||
        (uint64_t)(after_y - first_y) > UINT32_MAX) {
      oc_close_face(&context);
      return OC_TEXT_CAPACITY;
    }
    bounds[index].x = (int32_t)first_x;
    bounds[index].y = (int32_t)first_y;
    bounds[index].width = (uint32_t)(after_x - first_x);
    bounds[index].height = (uint32_t)(after_y - first_y);
  }
  oc_close_face(&context);
  return OC_TEXT_OK;
}

static int oc_copy_bitmap(FT_Glyph glyph, int64_t base_x, int64_t base_y,
                          const oc_text_glyph_bounds *target, uint8_t *output) {
  if (glyph == NULL || target == NULL || output == NULL) {
    return OC_TEXT_INVALID;
  }
  if (FT_Glyph_To_Bitmap(&glyph, FT_RENDER_MODE_NORMAL, NULL, 1) != 0) {
    return OC_TEXT_LIBRARY;
  }
  FT_BitmapGlyph bitmap_glyph = (FT_BitmapGlyph)glyph;
  FT_Bitmap *bitmap = &bitmap_glyph->bitmap;
  if (bitmap->width == 0 || bitmap->rows == 0) {
    FT_Done_Glyph(glyph);
    return OC_TEXT_OK;
  }
  if (bitmap->pixel_mode != FT_PIXEL_MODE_GRAY || bitmap->num_grays != 256 ||
      bitmap->buffer == NULL) {
    FT_Done_Glyph(glyph);
    return OC_TEXT_UNSUPPORTED;
  }
  int64_t bitmap_x = base_x + bitmap_glyph->left;
  int64_t bitmap_y = base_y - bitmap_glyph->top;
  int pitch = bitmap->pitch;
  for (uint32_t row = 0; row < bitmap->rows; row++) {
    int64_t canvas_y = bitmap_y + row;
    if (canvas_y < target->y ||
        canvas_y >= (int64_t)target->y + target->height) {
      continue;
    }
    const uint8_t *source =
        pitch >= 0 ? bitmap->buffer + (size_t)row * (size_t)pitch
                   : bitmap->buffer +
                         (size_t)(bitmap->rows - 1 - row) * (size_t)(-pitch);
    for (uint32_t column = 0; column < bitmap->width; column++) {
      int64_t canvas_x = bitmap_x + column;
      if (canvas_x < target->x ||
          canvas_x >= (int64_t)target->x + target->width) {
        continue;
      }
      size_t destination =
          (size_t)(canvas_y - target->y) * target->width +
          (size_t)(canvas_x - target->x);
      output[destination] = source[column];
    }
  }
  FT_Done_Glyph(glyph);
  return OC_TEXT_OK;
}

int oc_text_raster_glyphs(const uint8_t *font_data, size_t font_size,
                          uint32_t face_index,
                          const oc_text_glyph_request *requests,
                          const oc_text_glyph_bounds *targets,
                          uint32_t request_count, uint8_t *fill,
                          uint8_t *outline, const uint32_t *offsets,
                          uint32_t byte_size) {
  if (requests == NULL || targets == NULL || request_count == 0 || fill == NULL ||
      offsets == NULL || offsets[0] != 0 || offsets[request_count] != byte_size) {
    return OC_TEXT_INVALID;
  }
  oc_text_face context;
  int status = oc_open_face(font_data, font_size, face_index, 0, 0, &context);
  if (status != OC_TEXT_OK) {
    return status;
  }
  for (uint32_t index = 0; index < request_count; index++) {
    uint64_t area = (uint64_t)targets[index].width * targets[index].height;
    if (targets[index].width == 0 || targets[index].height == 0 ||
        area != offsets[index + 1] - offsets[index]) {
      oc_close_face(&context);
      return OC_TEXT_INVALID;
    }
    FT_Glyph fill_glyph = NULL;
    FT_Glyph outline_glyph = NULL;
    int64_t base_x = 0;
    int64_t base_y = 0;
    status = oc_glyph_pair(&context, &requests[index], &fill_glyph,
                           &outline_glyph, &base_x, &base_y);
    if (status != OC_TEXT_OK) {
      oc_close_face(&context);
      return status;
    }
    status = oc_copy_bitmap(fill_glyph, base_x, base_y, &targets[index],
                            fill + offsets[index]);
    if (status == OC_TEXT_OK && requests[index].outline_26_6 > 0) {
      if (outline == NULL || outline_glyph == NULL) {
        status = OC_TEXT_INVALID;
      } else {
        status = oc_copy_bitmap(outline_glyph, base_x, base_y, &targets[index],
                                outline + offsets[index]);
        outline_glyph = NULL;
      }
    }
    if (outline_glyph != NULL) FT_Done_Glyph(outline_glyph);
    if (status != OC_TEXT_OK) {
      oc_close_face(&context);
      return status;
    }
  }
  oc_close_face(&context);
  return OC_TEXT_OK;
}
