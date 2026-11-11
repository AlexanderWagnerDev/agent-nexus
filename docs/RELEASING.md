# Releasing

AgentNexus is in alpha. **There is no official version.** Do not create a tag,
publish a release or push a `latest` container tag until the maintainer has
explicitly approved the first public version.

## Current policy

- Every previous tag and GitHub release was removed on purpose. Do not recreate
  `v0.1.0-rc.*`, `v0.2.0-rc.*` or `v0.3.*` for any reason.
- Ordinary commits and pull requests must not produce a GitHub release. The
  release workflow is `workflow_dispatch` only and refuses to run unless the
  operator types `PUBLISH` and names an existing tag.
- `goreleaser` is configured with `release.draft: true`, so even a manual run
  produces a draft that must be published by hand.
- Container builds on `main` publish only `alpha` (moving) and `sha-<short>`
  (pinned) tags. There is no `latest` and no semver tag.
- The hub and daemon report their version as `alpha` unless a build sets a
  version through `-ldflags`.

## Before the first public version

1. The maintainer personally reviews the UI, security model and installation
   path and gives explicit approval.
2. The privacy checklist in `docs/PRIVACY.md` is complete and the git history
   has been rewritten with the approved public identity.
3. `go test ./...`, `go vet ./...`, the race detector, the JavaScript tests, the
   browser E2E suite and the accessibility scan pass on the release commit.
4. A backup created by the previous build restores cleanly into the candidate.
5. The release notes, screenshots and README contain no personal data.
6. The container image digest, checksums and SBOMs are recorded.

## Manual release procedure (only after approval)

1. Create and push the approved tag, for example `v0.1.0`.
2. In GitHub Actions, run **Release (manual, locked)** for that tag with
   `confirm=PUBLISH`.
3. Wait for the draft release. Review assets, checksums, SBOMs and the cosign
   bundle.
4. Publish the draft manually and mark it as the first official release.
5. Only then may the container workflow be extended with a semver tag, and the
   `latest` tag may be introduced.

## Versioning

The first approved version is expected to be `v0.1.0`. Until then, builds are
identified by commit SHA. Do not infer or promise a version number from this
document.
