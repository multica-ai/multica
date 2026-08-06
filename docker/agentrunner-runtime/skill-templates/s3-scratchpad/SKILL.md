---
name: s3-scratchpad
description: Use when a task needs to persist a file that outlives one issue comment, hand a human a downloadable link without a full attachment flow, or share a larger/multi-file artifact across the workspace. Covers uploading, listing, reading back, and presigning objects under this workspace's private agent-scratchpad/<slug>/ S3 prefix via the AWS CLI. Trigger on any task mentioning S3, scratchpad, shared drive, presigned link, or "share this file with a human".
---

# S3 Scratchpad Skill

**Trigger on any task that needs a durable, shareable file store beyond a single issue comment.**

This skill covers the workspace's private object-storage prefix on
`g2-agentfarm-dev-uploads`, reachable directly from this pod via IRSA — no
setup, no credentials to manage.

## What this is

- Every workspace has its own private prefix:
  `s3://g2-agentfarm-dev-uploads/agent-scratchpad/$WORKSPACE_SLUG/`
- Authentication is automatic (IRSA) — do not set `AWS_ACCESS_KEY_ID` /
  `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` anywhere; static keys silently
  outrank IRSA and would break access.
- Access is confined to this one prefix. Another workspace's prefix, and the
  bucket's `workspaces/<workspace-id>/...` area (backend-managed issue
  attachments), both return `AccessDenied` — that is by design, not a bug to
  troubleshoot.
- `$WORKSPACE_SLUG` is already in this pod's environment. Don't hardcode a
  slug you've seen elsewhere; use the variable so this works in every
  workspace unchanged.

## When to use this vs. `multica issue comment add --attachment`

Use `multica issue comment add <issue-id> --attachment <path>` (or
`issue create --attachment`) when the file is a single artifact tied to one
issue or comment — a screenshot, a log snippet, a small report. That path is
backend-mediated, shows up inline in the issue UI, and is the right default.

Use this S3 scratchpad instead when:
- the artifact should outlive or span multiple comments/issues (a working
  data file, a build output referenced across a few tasks),
- there are multiple related files better browsed as a set than attached one
  by one,
- the file is large enough that repeated attachment uploads are wasteful, or
- a human needs a direct download link independent of the Multica UI.

These are not mutually exclusive: it is fine to upload to the scratchpad and
still drop a presigned link in a comment for visibility.

## Verify identity (once per session, optional)

```bash
aws sts get-caller-identity
```

Expect an assumed-role ARN containing `agentfarm-agentrunner-role-<slug>` (or
`-dev-<slug>`). If this fails with `InvalidClientTokenId` or
`UnrecognizedClientException`, do not attempt to supply your own AWS
credentials — report it; it means IRSA is misconfigured for this pod, not
that you should route around it.

## Upload

```bash
aws s3 cp ./report.pdf "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/report.pdf"
```

**There is no delete permission, and `PutObject` silently overwrites an
existing key with no server-side protection.** Both facts point the same
way: pick a key you won't collide with rather than relying on being able to
delete a mistake or on a previous object staying intact. Prefer a
timestamp or short content-hash suffix for anything you might re-upload:

```bash
key="agent-scratchpad/${WORKSPACE_SLUG}/report-$(date +%Y%m%dT%H%M%S).pdf"
aws s3 cp ./report.pdf "s3://g2-agentfarm-dev-uploads/${key}"
```

## List

```bash
aws s3 ls "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/"
```

A bare `aws s3 ls s3://g2-agentfarm-dev-uploads/` (no prefix) will fail with
`AccessDenied` — that's expected, not a sign anything is broken.

## Read back / download

```bash
aws s3 cp "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/report.pdf" ./report.pdf
# or stream to stdout:
aws s3 cp "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/report.pdf" -
```

## Check size/metadata without downloading

```bash
aws s3api get-object-attributes \
  --bucket g2-agentfarm-dev-uploads \
  --key "agent-scratchpad/${WORKSPACE_SLUG}/report.pdf" \
  --object-attributes ObjectSize
```

Useful for picking the right file among several candidates before paying for
a full download.

## Share a file with a human (presigned link)

```bash
aws s3 presign "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/report.pdf" --expires-in 900
```

Paste the resulting `https://...` URL directly into an issue comment or chat
— it is a normal external link and will not trip the local-path-link guard
(that guard only blocks `file://` URLs and local filesystem paths, never
`https://`).

**Two independent clocks, and the shorter one wins.** `--expires-in` sets how
long the *signature* is valid, but the signature is computed from this
session's temporary IRSA/STS credentials — once those credentials themselves
expire, the link stops working even if its own `--expires-in` window hasn't
elapsed yet. Do not request a long `--expires-in` (hours) expecting a
long-lived link on that basis alone. Keep it short — under an hour — and
regenerate on demand if the human needs it again later; do not try to mint
one durable link up front.

## What you cannot do here, and why

- **No delete.** `s3:DeleteObject` is intentionally not granted — this is a
  read-and-upload prefix, not a scratch disk you can clean up. Don't build a
  workflow around deleting or replacing files in place; use new keys instead
  (see Upload, above).
- **No cross-workspace or bucket-wide access.** Every permission is scoped to
  `agent-scratchpad/${WORKSPACE_SLUG}/*` on this one bucket. This is enforced
  by IAM, not a client-side convention — attempting to reach outside it
  always fails with `AccessDenied`.
