# Git history rewrite (executed)

**Status: completed on 2026-11-11.** The repository history was rewritten with
`git filter-repo`; `main` was force-pushed with `--force-with-lease` after a
verified mirror backup. No tags or releases existed at that point.

## Why it was needed

Earlier commits carried a personal display name and personal email addresses in
their author and committer metadata. Public Git history exposes those fields
even after the current files are cleaned. A repository that is made public with
the existing history would therefore disclose personal data that was not
present in the working tree.

Earlier commits also contained screenshots and release notes from a real
development machine with real host names, project names and local paths. Those
files were replaced or deleted in the working tree, but the old blobs remained
reachable in history.

The exact values are intentionally **not** written down in this file.

## What was done

1. Created a mirror backup outside the repository
   (`agent-nexus-backup.git`).
2. Wrote a private mailmap outside the repository that maps every historical
   personal identity to
   `AlexanderWagnerDev <207699914+AlexanderWagnerDev@users.noreply.github.com>`.
3. Replaced the personal display name in all historical text blobs with
   `AlexanderWagnerDev`.
4. Removed all historical `docs/screenshots/*` and
   `docs/RELEASE-NOTES-*` blobs, because the old versions contained real
   development data or described withdrawn releases.
5. Re-added the 42 synthetic screenshots in one commit.
6. Force-pushed `main` with `--force-with-lease`.
7. Deleted all 211 old workflow runs and their logs.

## Verification

- `git log --format='%an <%ae>%n%cn <%ce>' | sort -u` lists only
  `AlexanderWagnerDev`, the GitHub merge committer and the two dependency bots.
- A scan for the personal display name across every reachable commit returns
  nothing.
- `git log --all --diff-filter=A -- docs/screenshots` shows only the synthetic
  screenshot commit.
- The final tree hash after re-adding the screenshots matched the pre-rewrite
  tree exactly (`08df77f72a6c8c37c50c9a1753e376ebd2633035`).
- No tags or releases were created during the rewrite.

## Residual notes

- Old commit SHAs no longer resolve in the repository. External links to them
  are dead, which is expected.
- GitHub may retain unreachable objects and still serve old commits by SHA for
  a while after a force-push. This was observed through the API. Before the
  repository is made public, ask GitHub Support to purge unreachable objects
  and cached views, and provide the old `main` SHA plus the rewritten SHA.
- The mirror backup must stay offline and private until the maintainer confirms
  the result, then be deleted.
