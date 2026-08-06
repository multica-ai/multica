---
name: s3-scratchpad
description: Use when a task needs to persist a file that outlives one issue comment, or share a larger/multi-file artifact across the workspace. Covers uploading, listing, reading back, and presigning objects under this workspace's private agent-scratchpad/<slug>/ S3 prefix via the AWS CLI. Trigger on any task mentioning S3, scratchpad, shared drive, presigned link, or "share this file with a human".
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

These are not mutually exclusive: it is fine to upload to the scratchpad for
durability and *also* attach the file to a comment for visibility. Note that
"a human needs a direct download link" does **not** mean pasting a presigned
URL into a comment — that does not survive delivery. See *Share a file with a
human*, below.

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

## Share a file with a human — upload it, don't presign it

**A presigned S3 URL does not survive delivery through Multica. Do not paste
one into a comment or a chat reply.** Every agent-authored comment and chat
body is passed through the platform's secret redactor
(`server/pkg/redact`, applied in `server/internal/service/task.go`). One of
its rules scrubs generic `TOKEN=<value>` patterns, and every presigned URL
carries an `X-Amz-Security-Token=` query parameter — so the rule fires on
your own link. Because the value pattern is `\S+`, it eats the token *and
every query parameter after it*, leaving:

```
https://...&X-Amz-SignedHeaders=host&X-Amz-Security-[REDACTED CREDENTIAL]
```

`X-Amz-Signature` sits past that point, so the human receives an unsignable
URL that returns an S3 error. This is silent from your side: `aws s3 presign`
exited 0, and you cannot see the redacted form of your own output.

Deliver the bytes instead. Download from the scratchpad, then hand the file to
the surface's own attachment mechanism:

```bash
aws s3 cp "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/report.pdf" ./report.pdf

# on an issue:
multica issue comment add <issue-id> --content-file ./body.md --attachment ./report.pdf
# in a chat session:
multica attachment upload ./report.pdf
```

Both produce an `https://agentfarm.g2.com/api/attachments/...` link with no
signed query parameters, so there is nothing for the redactor to match. That
link is also authenticated and outlives your session, which a presigned URL
does not.

**Do not try to route around the redactor.** Splitting the token across
lines, base64-ing the URL, renaming the parameter, or posting the query
string in fragments for the human to reassemble all defeat a deliberate
security control — and the URL it protects grants unauthenticated read access
to the object to anyone who ends up holding it. If a human explicitly asks
for a presigned URL anyway, generate it, then tell them in words that the
redactor will mangle it in this channel and offer the attachment instead.

`aws s3 presign` is still the right tool when the URL is consumed inside your
own run — handing it to a `curl` in a later step, or to a service that fetches
by URL — where nothing redacts it:

```bash
aws s3 presign "s3://g2-agentfarm-dev-uploads/agent-scratchpad/${WORKSPACE_SLUG}/report.pdf" --expires-in 900
```

**For that in-run use, two independent clocks apply and the shorter one
wins.** `--expires-in` sets how long the *signature* is valid, but the
signature is computed from this session's temporary IRSA/STS credentials —
once those credentials themselves expire, the link stops working even if its
own `--expires-in` window hasn't elapsed yet. Do not request a long
`--expires-in` (hours) expecting a long-lived link on that basis alone. Keep
it short — under an hour — and regenerate on demand; do not try to mint one
durable link up front.

## What you cannot do here, and why

- **No delete.** `s3:DeleteObject` is intentionally not granted — this is a
  read-and-upload prefix, not a scratch disk you can clean up. Don't build a
  workflow around deleting or replacing files in place; use new keys instead
  (see Upload, above).
- **No cross-workspace or bucket-wide access.** Every permission is scoped to
  `agent-scratchpad/${WORKSPACE_SLUG}/*` on this one bucket. This is enforced
  by IAM, not a client-side convention — attempting to reach outside it
  always fails with `AccessDenied`.
- **No handing a presigned URL to a human through Multica.** The secret
  redactor strips the signing parameters out of any comment or chat body you
  write, and it does so silently. Attach the file instead — see *Share a file
  with a human*, above.
