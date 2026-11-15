# Releasing

AgentNexus **v0.1.1** is the current release (prepared 2026-11-15);
**v0.1.0** was the first official release (published 2026-11-15).
Later tags and releases still require the maintainer's explicit approval.

## Current policy

- Older withdrawn tags and releases (`v0.1.0-rc.*`, `v0.2.0-rc.*`, `v0.3.*`)
  stay removed. Do not recreate them.
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
- Container builds on `main` publish `alpha` (moving) and `sha-<short>`
  (pinned) tags. Official releases use an explicit **Container** workflow
  dispatch with the `release_tag` input, which publishes the matching
  `v0.1.1`, `0.1.1`, `0.1` and `alpha` tags; the `latest` tag additionally
  requires `publish_latest=true`. `alpha` keeps moving with `main` after the
  release.
- The hub, daemon and terminal client report their version as `alpha` unless a
  build sets a version through `-ldflags`. Development builds use
  `alpha-<short-sha>`; official builds use the semantic version without the
  leading `v`.

## Published releases

| Version | Date | Artifacts |
| --- | --- | --- |
| v0.1.1 | 2026-11-15 (prepared) | Tag, archives and images pending the manual release workflow |
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

1. Create and push the approved tag, for example `v0.1.1`.
2. In GitHub Actions, run **Release (manual, locked)** for that tag with
   `confirm=PUBLISH`.
3. Wait for the draft release. Review assets, checksums, SBOMs and the cosign
   bundle. The archive names are
   `agent-nexus_<version>_<os>_<arch>.tar.gz` or `.zip` on Windows, which is
   exactly what the host updater expects.
4. Publish the draft manually.
5. Then run **Container** with `release_tag=v0.1.1` and `publish_latest=true`
   for a stable release. The dispatch publishes `v0.1.1`, `0.1.1`, `0.1`,
   `alpha` and `latest`; without `publish_latest` it publishes the first four.
   Dispatch it either from `main` while `HEAD` is still the tagged commit, or
   from the tag itself; both are accepted, while a stale `main` is rejected.
   Ordinary `main` builds keep publishing `alpha`/`sha-*` and can never
   overwrite the version tags.
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

The first approved version was `v0.1.0`; the current release is `v0.1.1`.
Builds without a release tag are identified by commit SHA. Do not infer or
promise a later version number from this document.
