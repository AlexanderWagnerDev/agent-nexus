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
  produces a draft that must be published by hand.
- Container builds on `main` publish only `alpha` (moving) and `sha-<short>`
  (pinned) tags. There is no `latest` and no semver tag.
- The hub and daemon report their version as `alpha` unless a build sets a
  version through `-ldflags`.

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
   bundle.
4. Publish the draft manually and mark it as the first official release.
5. Only then may the container workflow be extended with a semver tag, and the
   `latest` tag may be introduced.

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
