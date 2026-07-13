# Release bundle contract

Status: Day 0 baseline.

The activation and rollback unit is `release-bundle.tar.zst`:

```text
manifest.json
launcher/<platform pack>
payload/<opaque full application pack>
```

For Open Cut the payload is assembled from `apps/web`, `apps/api`, and
`apps/electron` into a full Electron pack. That source graph is build-plane
knowledge and is absent from launcher state and release manifests.

`oc-control pack` discovers every non-carrier `apps/*/sidecar/index.ts`, requires
its compiled `dist/sidecar/index.js`, production-deploys each app independently,
and writes one generated `payload-topology.json`. Electron consumes that artifact;
there is no second hand-maintained list of app entries.

The opaque payload currently contains this carrier-owned shape (platform wrapper
details omitted):

```text
payload/app/
  <Electron executable and frameworks>
  resources/app/                         # Electron root runtime
  resources/payload/payload-topology.json
  resources/payload/sidecars/<app>/
    dist/sidecar/index.js                 # sole compiled sidecar entry
    node_modules/                         # app-scoped production dependencies
```

The packer preserves only relative symlinks that remain inside the full-pack
root. A `pnpm deploy` self-reference that points back to the source workspace is
recognized by exact package name and removed; any other escaping link rejects the
build. The resulting release cannot depend on the build machine's checkout.

Canonical versions strictly match `X.Y.Z-<channel>.N`, including stable versions
such as `1.2.3-stable.4`. A display projection may render stable as `1.2.3`, but
display values never drive signatures, paths, ordering, state, or logs.

## Detached verification

Compressed-byte digest and signature metadata must be outside the archive to
avoid a circular self-digest:

```text
metadata/root.json
metadata/<channel>/<platform>-<arch>/latest.json
releases/<version>/<platform>-<arch>/release.json
releases/<version>/<platform>-<arch>/open-cut-<version>-<platform>-<arch>.release-bundle.tar.zst
```

`release.json` is a signed envelope containing schema, channel, canonical
version, public platform, public architecture, bundle size, SHA-256,
origin-relative bundle path, minimum bootstrap protocol, and publication time.
`manifest.json` contains launcher and payload entries plus the extracted tree contract.

The only public target values are `mac|win|linux` and `arm64|x64`; internal Go
values (`darwin`, `windows`, `amd64`) are adapter details. Immutable release
metadata and bundle bytes are target-scoped. Re-running publication for the same
content preserves the existing immutable `release.json`; conflicting content is
rejected.

B0 verifies trusted metadata and compressed bytes before extraction. Extraction
is implemented in Go, rejects absolute/traversing paths, special files, and
escaping links, and stages under `incoming/<transaction-id>` on the same
filesystem as `versions/`. Promotion is one rename.

Ed25519 trust roots are injected at bootstrap and persisted as local root version
1 before the first network update. `metadata/root.json` is an envelope containing
the next root. Rotation must advance exactly one version and meet the current
root's distinct-key signature threshold; the new root cannot authorize itself.
Downgrades, version gaps, invalid thresholds, duplicate key IDs, and malformed
keys are rejected. Release metadata is verified with the persisted root after
any accepted rotation. Unknown non-critical fields are additive; unsupported
critical fields reject only the candidate and keep last-good bootable.

Day 0 development macOS packs are ad-hoc signed after electron-builder produces
the full directory. Distribution identities, notarization, Windows signing, and
Linux repository credentials remain injected pipeline concerns and are not
launcher state.
