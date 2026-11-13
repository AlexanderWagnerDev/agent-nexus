# Releasing

AgentNexus is in alpha. **There is no official version.** Do not create a tag,
publish a release or push a `latest` container tag until the maintainer has
explicitly approved the first official version.

## Current policy

- Every previous tag and GitHub release was removed on purpose. Do not recreate
  `v0.1.0-rc.*`, `v0.2.0-rc.*` or `v0.3.*` for any reason.
- Ordinary commits and pull requests must not produce a GitHub release. The
  release workflow is `workflow_dispatch` only and refuses to run unless the
  operator types `PUBLISH` and names an existing tag.
- `goreleaser` is configured with `release.draft: true`, so even a manual run
  produces a draft that must be published by hand. `release.prerelease: auto`
  keeps stable tags stable and only marks a `-rc`/`-beta`/`-alpha` tag as a
  prerelease.
- Release archives contain `nexus-hub`, `nexusd` and the `nexus` terminal
  client for Windows, Linux and macOS on `amd64` and `arm64`, plus
  `checksums.txt`, per-archive SBOMs and the cosign bundle
  `checksums.txt.sigstore.json`.
- Container builds on `main` publish only `alpha` (moving) and `sha-<short>`
  (pinned) tags. There is no `latest` and no semver tag. A version tag is
  published only through an explicit **Container** workflow dispatch with the
  `release_tag` input; the `latest` tag additionally requires
  `publish_latest=true`.
- The hub, daemon and terminal client report their version as `alpha` unless a
  build sets a version through `-ldflags`. Development builds use
  `alpha-<short-sha>`; official builds use the semantic version without the
  leading `v`.

## Release decision

The repository is intentionally **public during alpha**. An official version
requires the maintainer's **explicit approval**. A fixed-duration load test,
personally performed end-to-end test, formal accessibility certification or
third-party penetration test is **not** a condition for publication. Engineering
milestones and verification records are informational, not release gates.

Normal CI, privacy safeguards, backups and artifact integrity remain part of
the regular development and distribution process. Describe actual limitations
accurately without turning optional testing into release blockers.

## Manual release procedure (only after approval)

1. Create and push the approved tag, for example `v0.1.0`.
2. In GitHub Actions, run **Release (manual, locked)** for that tag with
   `confirm=PUBLISH`.
3. Wait for the draft release. Review assets, checksums, SBOMs and the cosign
   bundle. The archive names are
   `agent-nexus_<version>_<os>_<arch>.tar.gz` or `.zip` on Windows, which is
   exactly what the host updater expects.
4. Publish the draft manually and mark it as the first official release.
5. Then run **Container** with `release_tag=v0.1.0` (and `publish_latest=true`
   only after a deliberate decision). Ordinary `main` builds keep publishing
   `alpha`/`sha-*` and can never overwrite the version tags.
6. Verify a host update on a disposable installation before announcing it.

## Leftover artifacts from removed releases

Removing Git tags and GitHub releases does not delete container images or old
workflow logs. At the time of the cleanup:

- GitHub Actions artifacts and all old workflow runs were deleted, including
  their logs.
- GHCR may still contain images from the removed release candidates. Listing
  and deleting them requires a token with `read:packages` and
  `delete:packages`:

  ```bash
  gh auth refresh -s read:packages,delete:packages
  gh api /user/packages/container/agent-nexus/versions --paginate
  gh api -X DELETE /user/packages/container/agent-nexus/versions/<id>
  ```

  Keep the current `alpha`/`sha-` images used for development tests. Do not
  delete packages that belong to another repository.

## Versioning

The first approved version is expected to be `v0.1.0`. Until then, builds are
identified by commit SHA. Do not infer or promise a version number from this
document.
