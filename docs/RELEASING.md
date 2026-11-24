# Releasing

AgentNexus **v0.1.17** is the current release (published 2026-11-24);
**v0.1.16** was published on 2026-11-21, **v0.1.15** on 2026-11-18,
**v0.1.14** on 2026-11-18, **v0.1.13** on 2026-11-17,
**v0.1.12** on 2026-11-17, **v0.1.11** on 2026-11-17,
**v0.1.10** on 2026-11-17, **v0.1.9** on 2026-11-17,
**v0.1.8** on 2026-11-16,
**v0.1.6** on 2026-11-16, **v0.1.5** on 2026-11-16, **v0.1.4** on 2026-11-16,
**v0.1.3** on 2026-11-16, **v0.1.2** on 2026-11-16, **v0.1.1** on 2026-11-15, and
**v0.1.0** was the first official release (published 2026-11-15). **v0.1.7** was
withdrawn and its release and tag were removed. Later tags and releases still
require the maintainer's explicit approval.

## Current policy

- Older withdrawn tags and releases (`v0.1.7`, `v0.1.0-rc.*`, `v0.2.0-rc.*`,
  `v0.3.*`) stay removed. Do not recreate them.
- Ordinary commits and pull requests must not produce a GitHub release. The
  release workflow is `workflow_dispatch` only. Dispatch it from `main` with the
  approved tag; the guard job creates and pushes the annotated tag at the
  dispatched commit and then re-dispatches the workflow from that tag. Only the
  tag-started run publishes, because cosign keyless identity is derived from
  the ref that started the run and the installers and updater require
  `.github/workflows/release.yml@refs/tags/<tag>`. Dispatching from `main` with
  an existing tag republishes that tag.
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
  `v0.1.17`, `0.1.17`, `0.1` and `alpha` tags; the `latest` tag additionally
  requires `publish_latest=true`. `alpha` keeps moving with `main` after the
  release.
- The hub, daemon and terminal client report their version as `alpha` unless a
  build sets a version through `-ldflags`. Development builds use
  `alpha-<short-sha>`; official builds use the semantic version without the
  leading `v`.

## Published releases

| Version | Date | Artifacts |
| --- | --- | --- |
| [v0.1.17](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.17) | 2026-11-24 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.17`, `0.1.17`, `0.1`, `alpha`, `latest` (digest `sha256:cdb133456b8f7bc8c411f80db2843c99ede30d34f07cab28b7a098498ca4b599`) |
| [v0.1.16](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.16) | 2026-11-21 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.16`, `0.1.16`, `0.1`, `alpha`, `latest` |
| [v0.1.15](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.15) | 2026-11-18 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.15`, `0.1.15`, `0.1`, `alpha`, `latest` (digest pending) |
| [v0.1.14](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.14) | 2026-11-18 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.14`, `0.1.14`, `0.1`, `alpha`, `latest` (digest `sha256:d04808ccea2f9e1502bb8a1872adc9d2cb6ea3fed36ae7dd7409ffc3c3704157`) |
| [v0.1.13](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.13) | 2026-11-17 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.13`, `0.1.13`, `0.1`, `alpha`, `latest` (digest `sha256:7bcae952f08cbafc7982eeec4f98d0f3b9efb88e3af4d46385279d84dcd36d9f`) |
| [v0.1.12](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.12) | 2026-11-17 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.12`, `0.1.12`, `0.1`, `alpha`, `latest` (digest `sha256:63d42c625b9810fc52fbedbdc175abd87782652c42c07365f91d23b5950a27a8`) |
| [v0.1.11](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.11) | 2026-11-17 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.11`, `0.1.11`, `0.1`, `alpha`, `latest` (digest `sha256:bf466d80224a447664a67d9891c383439215d419289a78c1bbd4aa81793f97ae`) |
| [v0.1.10](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.10) | 2026-11-17 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.10`, `0.1.10`, `0.1`, `alpha`, `latest` (digest `sha256:185cfdc1fc139b5e93b846e7c6f586b5bdab5ec3d474e7c3f4d291fda0bc533b`) |
| [v0.1.9](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.9) | 2026-11-17 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.9`, `0.1.9`, `0.1`, `alpha`, `latest` (digest `sha256:7d069601396e486005d0c2c1d108904f82a1b9df83158edf522a5b59d43bb3c5`) |
| [v0.1.8](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.8) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.8`, `0.1.8`, `0.1`, `alpha`, `latest` (digest `sha256:b06699e8e37b0b041970c565037ba87a012db6e3a7471ecfb7eb142ef6579d36`) |
| [v0.1.6](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.6) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.6`, `0.1.6`, `0.1`, `alpha`, `latest` (digest `sha256:fe421ba56fbeb608791250986896844bdae3cb84a39332497760e1a75702c44e`) |
| [v0.1.5](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.5) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.5`, `0.1.5`, `0.1`, `alpha`, `latest` (digest `sha256:e6d832545502ba7e4832734fbf18e55e857a9c90480adb19cc3554795e2c946e`) |
| [v0.1.4](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.4) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.4`, `0.1.4`, `0.1`, `alpha`, `latest` (digest `sha256:b21cd84613114413ef9b8f9dd3e02f965cc985a56f3c10cd636c946b174877f7`) |
| [v0.1.3](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.3) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.3`, `0.1.3`, `0.1`, `alpha`, `latest` (digest `sha256:a217df4627b032992bc68e5ea58a49e84149a2d174d6f1bd64949e0ea487c14d`) |
| [v0.1.2](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.2) | 2026-11-16 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.2`, `0.1.2`, `0.1`, `alpha`, `latest` (digest `sha256:7061c549f2402b0d36c14dfcd5c3605c603bf6d30009ca9d5936a53238e385da`) |
| [v0.1.1](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.1) | 2026-11-15 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.1`, `0.1.1`, `0.1`, `alpha`, `latest` (digest `sha256:b8716fdea47d4e0efb346a59fcb2dbb9baf2514eac9499b0c034e43b0d8b734c`) |
| [v0.1.0](https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.0) | 2026-11-15 | GitHub release archives, checksums, SBOMs, cosign bundle; Docker Hub and GHCR images `v0.1.0`, `0.1.0`, `latest`, `alpha` |

> **Withdrawn:** `v0.1.7` was published briefly on 2026-11-16 and removed the
> same day. Its Windows task started `nexusd.exe run --hidden`, but
> `GetConsoleWindow` was loaded from the wrong DLL and the daemon panicked.
> Its GitHub release and tag were removed; do not pin it. Use `v0.1.17`.
> Container tags produced from that build may still exist in the registries.

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

1. Run **Release** from `main` with the approved tag, for example
   `gh workflow run release.yml --ref main -f tag=v0.1.17`, and approve the
   run. The guard job validates the tag, creates and pushes the annotated tag
   at the dispatched `main` commit, then starts the workflow from that tag.
   The first run ends without publishing; the tag-started run builds and
   publishes. If the tag already exists, the guard job re-dispatches it
   instead. Existing release assets may have to be removed first because
   GitHub does not guarantee in-place replacement.
2. Review the published release's assets, checksums, SBOMs and the cosign
   bundle. The archive names are
   `agent-nexus_<version>_<os>_<arch>.tar.gz` or `.zip` on Windows, which is
   exactly what the host updater expects. If the run produced a broken
   release, delete the release and re-dispatch from `main`; the existing tag is
   reused. Delete the tag as well only when it must move to another commit.
3. Releases produced before this procedure (including v0.1.15) carry a
   `refs/heads/main` certificate identity and fail the tag-bound cosign check.
   Republish them from a recreated tag. On a machine with cosign installed the
   installers refuse such a release; removing cosign from `PATH` would fall
   back to SHA-256 only, but that skips the signature check. The post-install
   checksum-only options configure future auto-updates and do not bypass an
   install-time cosign failure.
4. Then run **Container** with `release_tag=v0.1.17` and `publish_latest=true`
   for a stable release. The dispatch publishes `v0.1.17`, `0.1.17`, `0.1`,
   `alpha` and `latest`; without `publish_latest` it publishes the first four.
   Dispatch it from `main` or from the tag; when `release_tag` is set the
   workflow checks out that tag before building, so a newer `main` does not
   block a release rebuild, while a non-main branch is rejected.
   Ordinary `main` builds keep publishing `alpha`/`sha-*` and can never
   overwrite the version tags.
5. Verify a host update on a disposable installation before announcing it.

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

The first approved version was `v0.1.0`; the current release is `v0.1.17`.
Builds without a release tag are identified by commit SHA. Do not infer or
promise a later version number from this document.
