#!/usr/bin/env bash
# Publishes the media-toolchain reuse keys for this job.
#
# The keys are derived by `oc-control media-cache-key`, which asks the same
# authorities the build itself consults - the pinned catalog and the renderer's
# real dependency closure as reported by the Go toolchain. Deriving them here
# from a hand-written list of paths would recreate exactly the drift the
# closure fingerprint exists to prevent: the list would quietly stop describing
# what the build actually reads, and a stale artifact would be restored with a
# key that claims it is current.
#
# ImageOS and ImageVersion identify the hosted runner image. Identical sources
# compiled against a different system compiler produce different bytes, so the
# image belongs in the closure key; a runner image upgrade then rebuilds
# exactly once, on the first job that sees it.
set -euo pipefail

if [ -z "${CONTROL:-}" ]; then
  echo "media cache key derivation requires CONTROL" >&2
  exit 1
fi

arguments=(media-cache-key --repo . --environment "${ImageOS:-unknown}/${ImageVersion:-unknown}")
if [ -n "${PLATFORM:-}" ] && [ -n "${ARCH:-}" ]; then
  arguments+=(--platform "$PLATFORM" --arch "$ARCH")
fi

keys="$("$CONTROL" "${arguments[@]}")"
python3 - "$keys" <<'PY' >> "$GITHUB_OUTPUT"
import json, sys
keys = json.loads(sys.argv[1])
for field, name in (
    ("sourcePrefix", "source-prefix"),
    ("sourceKey", "source-key"),
    ("closurePrefix", "closure-prefix"),
    ("closureKey", "closure-key"),
):
    value = keys[field]
    if not value:
        raise SystemExit(f"media cache key {name} is empty")
    print(f"{name}={value}")
PY
