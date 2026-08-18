# Dual-compression push

## Context

zstd:chunked layers make pulls dramatically faster, but the ecosystem cannot
switch outright: old Docker clients cannot pull zstd layers at all. Dual
compression is the migration strategy — every pushed image carries both
variants, and clients pick what they understand [ADR 0070].

To put it simply: `konflux-build-cli image build --push
--compression-format=dual` pushes both a gzip and a zstd:chunked variant of
the built image and bundles them in a per-arch OCI index. The index digest is
the primary result. The child manifest refs land in the `images` result
field, see [Results contract](#results-contract).

## How dual mode works

Dual compression requires us to layer additional logic over buildah's
elementary compression values to facilitate a multi-step image and manifest
push behaviour where plain `buildah push` would not suffice.

The flags first. `--compression-format` accepts `gzip`, `zstd:chunked` or
`dual`. The values are passed to buildah unchanged (`dual` is the only
addition on top of buildah's own values; it is a multi-push mode that buildah
does not have). Note that buildah defaults force-compression to true whenever
`--compression-format` is explicitly passed — konflux-build-cli keeps that
buildah behavior. The default is load-bearing for dual mode: the second
variant push must not reuse the blobs the first push uploaded, otherwise both
variants would be identical.

The push itself goes like this:

1. Push the image twice, to per-compression temporary tags:
   - `<tag>-gzip-<suffix>` (as a result of `--compression-format=gzip`)
   - `<tag>-zstd-<suffix>` (as a result of `--compression-format=zstd:chunked`)

   where suffix is a random per-build hex string, so concurrent builds
   pushing to the same repository cannot clobber each other's temporary tags.
2. Create a local manifest list named
   `localhost/kbc-dual-index-<hash(output-ref)>-<suffix>` and add both
   variants by their registry digests.

   **Note: gzip is always first due to old Docker clients' behaviour — they
   pick the first acceptable manifest.**

   The name must be unique per build: `buildah manifest create` fails if the
   name is already in use. The list is removed from local storage after the
   push (`buildah manifest rm`).
3. Annotate the zstd entry with `io.github.containers.compression.zstd=true`
   (`buildah manifest annotate`). Podman and CRI-O read this annotation to
   prefer the zstd variant. buildah does not set it when adding a manifest by
   registry reference (it only sets it for `manifest push
   --add-compression`), so we set it explicitly.
4. Push the list to the real tag with `buildah manifest push`. Plain
   `buildah push` cannot be used here: the children were added by registry
   digest and don't exist in local storage, so `buildah push` cannot resolve
   them. `manifest push` copies the children from the registry (the same flow
   as `image build-image-index`). Additional tags are pushed the same way.
   The blobs are already in the registry, so these are pure manifest copies.
5. The real tag therefore only ever points at the final index, never at a
   single-compression manifest.

The temporary tags anchor the variants in the registry between steps 1 and 4.
The index references them by digest, so they are only needed until the index
push lands. They are not cleaned up afterwards: there is no registry-agnostic
way to delete tags, and skopeo's delete is known to be unreliable. If the
image carries a quay.io expiration label, the temporary tags point at single
manifests, so quay can read the label from their configs and expire them.
The real tag is a different story: quay reads expiration labels from the
manifest config, and an index has no config, so an index tag cannot expire
this way (true for any multi-arch image today, not just dual). What to do
about expiring index tags is follow-up work.

**Note: in single-compression modes, the push flow is unchanged and the
results JSON only gains the `images` field when dual mode produces child
manifests.**

## Results contract

* `image_url` [string]: the per-arch index reference (Tekton `IMAGE_URL` /
  `IMAGE_REF`).
* `digest` [string]: the per-arch index digest (Tekton `IMAGE_DIGEST`). This
  is the primary reference.
* `images` [string]: the child manifest references, digest-pinned and
  comma-separated (`gzip-ref,zstd-ref`) — the same format `build-image-index`
  uses for its own `images` result. The Tekton task exposes it as the
  `IMAGES` result for Chains provenance.

`--index-manifest-output <path>` is a flag, not a result. It writes the
per-arch index manifest JSON before the index push. `mobster generate
oci-index` takes this file as direct input (`--index-manifest-path`) to build
the per-arch index SBOM. The same data is also recoverable from the registry
afterwards — the zstd entry carries the compression annotation in the index.

## Constraints

* Dual requires an OCI index. A docker v2s2 manifest list cannot replace it:
  v2s2 descriptors only carry mediaType, size, digest and platform — there is
  no annotations field, so the zstd compression annotation could not be
  attached and Podman/CRI-O would have no way to prefer the zstd variant
  (ADR 0070 also notes that quay rejects mixed-format indexes). For that
  reason `compression-format=dual` with `push-format=docker` is rejected
  during validation. An unset `push-format` is fine — the built image
  defaults to oci.
* Dual derives the temporary tags from the output-ref tag, so the output-ref
  must be tagged.

[ADR 0070]: https://github.com/konflux-ci/architecture/blob/main/ADR/0070-dual-compression-for-container-builds.md
