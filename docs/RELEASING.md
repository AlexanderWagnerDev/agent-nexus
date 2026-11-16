# Releasing

AgentNexus **v0.1.5** is the current release (published 2026-11-16);
**v0.1.4** was published on 2026-11-16, **v0.1.3** on 2026-11-16,
**v0.1.2** on 2026-11-16, **v0.1.1** on 2026-11-15, and **v0.1.0** was the
first official release (published 2026-11-15). Later tags and releases still
require the maintainer's explicit approval.

## Current policy

- Older withdrawn tags and releases (`v0.1.0-rc.*`, `v0.2.0-rc.*`, `v0.3.*`)
  stay removed. Do not recreate them.
- Ordinary commits and pull requests must not produce a GitHub release. The
  release workflow is `workflow_dispatch` only, requires a semver tag and
  dispatches from `main` or from that tag. It creates and pushes the annotated
  tag on the dispatched `main` commit before goreleaser runs; an existing tag
  is published as-is.
- `goreleaser` publishes the release directly, without a draft stage.
  `release.prerelease: auto` keeps stable tags stable and only marks a
  `-rc`/`-beta`/`-alpha` tag as a prerelease.
- Release archives contain `nexus-hub`, `nexusd` and the `nexus` terminal
  client for Windows, Linux and macOS on `amd64` and `arm64`, plus
  `checksums.txt`, per-archive SBOMs and the cosign bundle
  `checksums.txt.sigstore.json`.
- Container builds on `main` publish `alpha` (moving) and `sha-<short>`
  (pinned) tags. Official releases use an explicit **Container** workflow
  dispatch with the `release_tag` input, which publishes the matching
  `v0.1.5`, `0.1.5`, `0.1` and `alpha` tags; the `latest` tag additionally
  requires `publish_latest=true`. `alpha` keeps moving with `main` after the
  release.
- The hub, daemon and terminal client report their version as `alpha` unless a
  build sets a version through `-ldflags`. Development builds use
  `alpha-<short-sha>`; official builds use the semantic version without the
  leading `v`.

## Published releases

| Version | Date | Artifacts |
| --- | --- | --- |
| [v0.1.5](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.5) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.5`, `0.1.5`, `0.1`, `alpha`, `latest` (digest `sha256:e6d832545502ba7e4832734fbf18e55e857a9c90480adb19cc3554795e2c946e`) |
| [v0.1.4](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.4) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.4`, `0.1.4`, `0.1`, `alpha`, `latest` (digest `sha256:b21cd84613114413ef9b8f9dd3e02f965cc985a56f3c10cd636c946b174877f7`) |
| [v0.1.3](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.3) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.3`, `0.1.3`, `0.1`, `alpha`, `latest` (digest `sha256:a217df4627b032992bc68e5ea58a49e84149a2d174d6f1bd64949e0ea487c14d`) |
| [v0.1.2](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.2) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.2`, `0.1.2`, `0.1`, `alpha`, `latest` (digest `sha256:7061c549f2402b0d36c14dfcd5c3605c603bf6d30009ca9d5936a53238e385da`) |
| [v0.1.1](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.1) | 2026-11-15 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.1`, `0.1.1`, `0.1`, `alpha`, `latest` (digest `sha256:b8716fdea47d4e0efb346a59fcb2dbb9baf2514eac9499b0c034e43b0d8b734c`) |
| [v0.1.0](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.0) | 2026-11-15 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.0`, `0.1.0`, `latest`, `alpha` |

## Release decision

The repository is intentionally **public**. An official version requires the
maintainer's **explicit approval**. A fixed-duration load test,
personally performed end-to-end test, formal accessibility certification or
third-party penetration test is **not** a condition for publication. Engineering
milestones and verification records are informational, not release gates.

Normal CI, privacy safeguards, backups and artifact integrity remain part of
the regular development and distribution process. Describe actual limitations
accurately without turning optional testing into release blockers.

## Manual release procedure (only after approval)

1. In GitHub Actions, run **Release** from `main` with the approved tag, for
   example `tag=v0.1.5`. The workflow creates and pushes the annotated tag on
   the dispatched commit and publishes the GitHub release. Re-running it for an
   existing tag publishes that tag as-is.
2. Review the published release's assets, checksums, SBOMs and the cosign
   bundle. The archive names are
   `agent-nexus_<version>_<os>_<arch>.tar.gz` or `.zip` on Windows, which is
   exactly what the host updater expects. If the run produced a broken
   release, delete the release and the tag before dispatching it again.
3. Then run **Container** with `release_tag=v0.1.5` and `publish_latest=true`
   for a stable release. The dispatch publishes `v0.1.5`, `0.1.5`, `0.1`,
   `alpha` and `latest`; without `publish_latest` it publishes the first four.
   Dispatch it from `main` or from the tag; when `release_tag` is set the
   workflow checks out that tag before building, so a newer `main` does not
   block a release rebuild, while a non-main branch is rejected.
   Ordinary `main` builds keep publishing `alpha`/`sha-*` and can never
   overwrite the version tags.
4. Verify a host update on a disposable installation before announcing it.

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

The first approved version was `v0.1.0`; the current release is `v0.1.5`.
Builds without a release tag are identified by commit SHA. Do not infer or
promise a later version number from this document.
