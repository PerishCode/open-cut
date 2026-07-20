#!/usr/bin/env bash
# Derives the two media-toolchain cache keys for one target.
#
# The archives and the built closure are cached separately because their inputs
# differ: which tarballs to fetch is decided by the pinned catalog alone, while
# the closure additionally embeds the renderer's own Go source. Sharing one key
# would re-download half a gigabyte of pinned fonts for every renderer edit.
#
# A prefix restore-key is safe here because nothing downstream trusts a restored
# tree on the strength of its key. Load rejects a closure whose manifest version
# is not the compiled-in toolchain version, the renderer source fingerprint
# rejects a stale helper, and ensureSource re-downloads any archive whose bytes
# do not match its pinned digest. A near-miss therefore starts warm and rebuilds
# exactly what actually changed.
set -euo pipefail

if [ -z "${TARGET:-}" ] || [ -z "${SOURCE_INPUTS:-}" ] || [ -z "${CLOSURE_INPUTS:-}" ]; then
  echo "media cache key derivation requires TARGET, SOURCE_INPUTS, and CLOSURE_INPUTS" >&2
  exit 1
fi

source_prefix="media-sources-v1-${TARGET}-"
closure_prefix="media-closure-v1-${TARGET}-"
{
  echo "source-prefix=${source_prefix}"
  echo "source-key=${source_prefix}${SOURCE_INPUTS}"
  echo "closure-prefix=${closure_prefix}"
  echo "closure-key=${closure_prefix}${CLOSURE_INPUTS}"
} >> "$GITHUB_OUTPUT"
