---
name: release
description: >-
  Release goagain — tag a version to build and push goagain-api/goagain-mcp
  images to GHCR. Use for "release", "ship", "publish", "cut a release", "deploy",
  "bump the version", or why a release missed new data. Commits merged data to
  main, picks the semver bump (no version file — git tag is source of truth),
  pushes the vX.Y.Z tag that triggers release.yml.
allowed-tools: Read, Grep, Bash
---

# Release skill

## Process

1. Land everything on `main` first. There is **no version file to edit**, the git tag is the only source of truth.
2. Pick the semver bump from the last tag (`git tag --sort=-creatordate | head -1`).
3. Tag and push; that's the whole release: `git tag vX.Y.Z && git push origin vX.Y.Z`.
4. `release.yml` builds both targets and pushes `X.Y.Z`, `X.Y`, `X`, and `latest` to `ghcr.io/oleiade/goagain-{api,mcp}`

## Gotchas

- The image embeds `internal/data/english/*.json` via `go:embed`. The release does **not** run `sync-data.sh` —
  if you refreshed data, commit those files to `main` before tagging or the release ships stale data.
  - Version is injected at build time (`-ldflags -X .../observability.Version=${VERSION}`); it defaults to `dev`
  when built without a tag. Nothing to bump in source.
  - The tag must match `v*` or `release.yml` won't fire.
  - `latest` moves on every release tag — fine if the server tracks `latest`, but pin an explicit tag if you
  don't want auto-rollout.
  - Tag an immutable commit on `main`; re-tagging a pushed version to fix a bad build means deleting the remote
  tag first.
