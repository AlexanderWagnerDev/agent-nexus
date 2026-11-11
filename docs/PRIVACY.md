# Privacy and public identity

AgentNexus is intended to become public. This document records the rules that
keep the repository, its artifacts and its screenshots free of the maintainer's
personal data.

## Public identity

| Field | Value |
| --- | --- |
| GitHub account | [AlexanderWagnerDev](https://github.com/AlexanderWagnerDev) |
| Repository | [AlexanderWagnerDev/agent-nexus](https://github.com/AlexanderWagnerDev/agent-nexus) |
| Public author | `AlexanderWagnerDev` |
| Copyright holder | `AlexanderWagnerDev` |
| Commit identity | `AlexanderWagnerDev <207699914+AlexanderWagnerDev@users.noreply.github.com>` |

The project uses the GitHub account name and the GitHub-provided no-reply
address. Do not add a legal name, a private email address, a company name or an
invented contact address to project files, package metadata, commits or
releases.

## Demo data rules

Screenshots, fixtures and examples must use synthetic values only:

- users and hosts: `demo`, `windows-dev`, `linux-builder`, `macbook-demo`
- projects: `sample-api`, `example-service`
- paths: `C:\Users\demo\Projects\sample-api`, `/home/demo/projects/example-service`
- domains: `nexus.example.com`
- IP addresses: only the reserved documentation ranges (`192.0.2.0/24`,
  `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`)

Never capture a real hostname, Windows user name, home directory, repository
remote, API key, session token, private chat transcript or production terminal
output. Record screenshots against a throwaway hub populated with synthetic
sessions.

## Current status

- Current project files use `AlexanderWagnerDev` for authorship and copyright.
- The old release notes and release links were removed together with the tags
  and GitHub releases.
- New screenshots must be generated from the synthetic demo hub in
  `web/e2e/demo/`. Existing screenshots are being replaced as the demo fixture
  grows.
- **Git history:** 13 commits carry a personal display name and 72 commits use a
  personal email domain in author/committer metadata. Earlier commits also
  contain screenshots from a real development machine. They are not in the
  current tree. A history rewrite is prepared but **not executed** because it
  changes every commit SHA and requires separate maintainer approval. See
  [HISTORY-REWRITE.md](HISTORY-REWRITE.md).

## Pre-publication checklist

1. `git log --format='%an <%ae>%n%cn <%ce>' | sort -u` shows only the approved
   identity, bots and GitHub.
2. A secret scan of the working tree and the full history is clean.
3. Every screenshot was produced from synthetic data and has no embedded
   metadata.
4. `README`, `LICENSE`, `CONTRIBUTING`, `SECURITY`, package metadata and the
   web client contain no legal name or private contact address.
5. No links point to deleted releases or tags.
6. The maintainer has explicitly approved making the repository public.
