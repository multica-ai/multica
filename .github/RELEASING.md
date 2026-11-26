# Release runbook

## Normal release

Release from a reviewed commit on `main` by creating and pushing a new semantic
version tag such as `v0.18.4`. The Release workflow intentionally has no manual
trigger: a tag push is the only event that can publish binaries, Homebrew
formulae, and container images.

The verification job runs the Go tests and `govulncheck` before any publishing
job starts. The vulnerability scan is fail-closed by default.

Release notes use GoReleaser's GitHub changelog to credit commit authors and
recognized co-authors between the previous and current tags. Keep all commits,
including documentation, tests, and chores, so their authors are credited too.
Squash commits take their message from the PR description, so co-authors are
recognized from the `Co-authored-by:` lines at its end.
Commit-subject `@` characters are separated from the following text with an HTML
comment; only the generated author mentions populate GitHub's Contributors list.

CI and release jobs require Go `~1.27.4`: at least 1.27.4, while accepting
newer 1.27 patches. This security floor is independent of the development
minimum in `server/go.mod`. A plain `1.27.x` can resolve to an older patch
while the `setup-go` version manifest catches up with an official Go release.
When raising the security floor, update all Go workflows and the pinned
Go builder image in `Dockerfile` together so shipped binaries also receive
the fixes.

## Emergency vulnerability-scan bypass

Use the bypass only when `govulncheck` itself or its live vulnerability database
is unavailable, or when maintainers have documented a confirmed false positive
that blocks an urgent release. Never use it to publish a release with an
unresolved reachable vulnerability.

1. Record the reason and maintainer approval in the release issue or pull
   request, and confirm no other release is in progress.
2. In **Settings → Secrets and variables → Actions → Variables**, set the
   repository variable `ALLOW_VULN_BYPASS_FOR_TAG` to the exact release tag,
   for example `v0.18.4`.
3. Re-run the failed Release workflow for that tag. A different tag, an empty
   value, or any typo keeps the scan enabled.
4. Confirm the verification log contains the explicit bypass warning and retain
   the workflow URL in the incident record.
5. Delete `ALLOW_VULN_BYPASS_FOR_TAG` immediately after the release run
   completes. The tag-scoped value prevents a concurrent release with another
   tag from inheriting the bypass.

Every Go binary retains its compiler version in the standard Go build metadata;
use `go version -m <binary>` when auditing a downloaded release artifact.

## Stable release promotion

GoReleaser publishes a public release with `make_latest: false`. Installed stable
clients and CLI entry points that read Latest keep using the previous complete
version while Desktop artifacts are uploading. Homebrew continues to publish
normally because CLI archives are already public; container tags are independent.
The release page and GitHub notifications can show the new release before promotion.

`.github/workflows/promote-release.yml` is the only entry point for changing
Latest. The Linux/Windows Desktop matrix calls it after both builds succeed.
After the manual macOS release has signed, notarized, verified, and uploaded both
architectures, the release-desktop skill dispatches the same workflow:

```bash
gh workflow run promote-release.yml --repo multica-ai/multica --ref main -f tag=vX.Y.Z
```

The workflow uses the reviewed validator from `main`, not executable code from the
requested tag. It serializes promotion globally and only promotes a stable
semantic version newer than the current Latest. Repeating a completed promotion
or finishing an older release after a hotfix leaves Latest unchanged.

The gate checks all six update metadata files (`latest-mac.yml`,
`latest-x64-mac.yml`, `latest.yml`, `latest-arm64.yml`, `latest-linux.yml`, and
`latest-linux-arm64.yml`), their version against the tag, and each referenced
artifact's architecture, upload status, and byte size. It also requires all six
CLI targets in both legacy and versioned archive names plus `checksums.txt`.
macOS update metadata must reference the ZIP; Windows requires EXE and Linux
AppImage. Missing blockmaps do not block promotion. The macOS `afterSign` hook
runs `spctl` and `stapler validate` after notarization and before packaging/upload
for explicit publishing builds. The release skill requires code signing so an
unsigned build cannot skip that hook. These host checks do not run for unsigned
local builds or other platforms. The gate does not download large binaries to
recalculate SHA-512.

Missing or inconsistent assets produce a `waiting` result with a list of blockers
and leave Latest unchanged. This is normal when macOS and CI finish in either
order. The next completed upload path tries again. API/authentication failures
fail the job. Inspect the promotion run result; a successful dispatch only means
the run was queued. If needed, correct the failed upload and dispatch the workflow
again. Do not bypass the gate with a direct `gh release edit --latest`.

Land this workflow on `main` before the first release using `make_latest: false`.
Until promotion succeeds, stable clients stay on the previous version. Existing
clients need no changes to benefit from the release ordering fix; friendly failure
messages and download recovery require the newly shipped Desktop client.

For a faulty release, publish a higher patch version through the same gate.
Do not move Latest backwards: already installed Windows arm64 clients predating
the downgrade guard may install the older binary. Removing `make_latest: false`
reintroduces the incomplete-release window and is not a version rollback.
