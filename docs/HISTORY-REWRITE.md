# Git history rewrite plan (prepared, not executed)

**Status: awaiting explicit maintainer approval.** Nothing in this document has
been run. No history has been rewritten and no force-push has been performed.

## Why it is needed

Earlier commits in this repository carry a personal display name and personal
email addresses in their author and committer metadata. Public Git history
exposes those fields even after the current files are cleaned. A repository that
is made public with the existing history would therefore disclose personal data
that is not present in the working tree.

Earlier commits also contain screenshots and text from a real development
machine with real host names, project names and local paths. Those files were
replaced in the working tree, but the old blobs remain reachable in history.

The exact values are intentionally **not** written down in this file. They are
listed in the private approval request.

## Impact

- Every rewritten commit gets a new SHA. Branches, tags, open pull requests and
  external forks diverge.
- There are currently no tags or releases (they were removed), which makes this
  the cheapest possible moment to rewrite.
- Commit signatures, if any, become invalid and must be re-signed or dropped.
- CI must be re-run on the rewritten `main`.
- GitHub may retain unreachable objects for a while; GitHub Support can be asked
  to purge cached views after a force-push.

## Proposed procedure (only after approval)

1. Freeze pushes and merges; announce a short maintenance window.
2. Create a mirror backup outside the repository:
   `git clone --mirror <repo> agent-nexus-backup.git`.
3. Write a private `.mailmap` **outside** the repository that maps every
   historical personal identity to
   `AlexanderWagnerDev <207699914+AlexanderWagnerDev@users.noreply.github.com>`.
4. Rewrite with `git filter-repo --mailmap <private-mailmap>` (or
   `--email-callback` when the display name also has to be normalised).
5. Purge historical screenshot blobs that came from the real development
   machine. Because the current tree uses new files, the simplest reliable
   option is to rebuild the public history from the cleaned tree while keeping
   the commit messages, or to run `git filter-repo --path docs/screenshots
   --invert-paths` followed by re-adding the synthetic screenshots in one
   commit.
6. Rewrite commit messages only if a secret scan finds personal data there.
6. Verify:
   - `git log --format='%an <%ae>%n%cn <%ce>' | sort -u` contains only approved
     identities and bots;
   - `git log --all --oneline | wc -l` matches the backup;
   - the tree of `main` is byte-identical to the pre-rewrite tree.
7. Run the full test suite, the browser E2E suite and the accessibility scan.
8. Force-push `main` with `--force-with-lease`.
9. Re-run CI, CodeQL and the container build.
10. Delete the mirror backup after the maintainer confirms the result, or keep
    it offline until the repository is public.

## Approval needed

The maintainer must approve, in writing:

- the rewrite itself,
- the force-push of `main`,
- the loss of the current commit SHAs,
- the handling of any open pull requests or forks.

Until then the history stays untouched and the repository must not be made
public.
