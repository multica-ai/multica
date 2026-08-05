# AWS access from agentrunner pods

How AWS calls made from an agent session authenticate, and what they are allowed
to reach. Background: AIPLAT-169.

One consumer: the **AWS CLI** bundled in the runner image, reading and writing
objects under the `agent-scratchpad/<workspace-slug>/` prefix of
`g2-agentfarm-dev-uploads` — each workspace gets its own prefix and its own
IAM role, so no workspace can reach another's scratch files.

## What was wrong

The platform's original plan was the `aws-s3` MCP server
(`awslabs.s3-tables-mcp-server`), whose entry carried static STS session
credentials (`AWS_ACCESS_KEY_ID` starting `ASIA`, plus secret and session token)
pinned into the agent's server-side `mcp_config`. Every call returned:

```
UnrecognizedClientException: The security token included in the request is invalid
```

`InvalidClientTokenId`, not `ExpiredToken` — AWS did not recognise the access
key ID at all. And nothing could pick up the slack, because the pod had no other
identity: the `agentrunner` ServiceAccount carried no `eks.amazonaws.com/role-arn`
annotation, `/var/run/secrets/eks.amazonaws.com/serviceaccount/` did not exist,
and IMDS (`169.254.169.254`) was unreachable.

Pinned session credentials also expire by construction, so even a freshly pasted
working set becomes an outage on its own schedule.

Underneath the credential problem sat a design problem: that MCP server speaks
only the **S3 Tables / Iceberg** API. It has no `get_object`, no `put_object`,
no key listing. What the platform actually wants from S3 is plain file storage,
which that server cannot do at any permission level. It is therefore retired —
no `s3tables:*` permissions remain in this role, and the MCP entry should be
removed from the agent config (see below).

## How it authenticates now

IRSA, the same mechanism the backend's avatar-upload path already uses — no
long-lived or human-refreshed credentials anywhere.

| | |
|---|---|
| Cluster | development EKS, `us-east-1` |
| Account | `975049976121` |
| OIDC provider | `oidc.eks.us-east-1.amazonaws.com/id/61752071A894EC141875218139B18C3F` |
| ServiceAccount | `agentrunner` in every `agentrunner-<slug>` / `agentrunner-dev-<slug>` namespace |
| Role | `arn:aws:iam::975049976121:role/agentfarm-agentrunner-role-<slug>` (tools pipeline) / `-dev-<slug>` (dev-server pipeline), one pair per workspace |
| Policy | `agentfarm-agentrunner-policy-<slug>` / `-dev-<slug>`, one pair per workspace |

Manifests:

- `gitops/base/agent-runtime/iam-agentrunner.yaml` — role, policy, attachment,
  templated per workspace (see below).
- `gitops/base/agent-runtime/service-account.yaml` — the `role-arn` annotation.
- `gitops/base/agent-runtime/deployment.yaml` — `AWS_REGION`. The IRSA webhook
  injects `AWS_ROLE_ARN` and `AWS_WEB_IDENTITY_TOKEN_FILE` but never a region,
  and the CLI has no default one.
- `docker/agentrunner-runtime/Dockerfile` — AWS CLI v2. It goes in the
  agentfarm-specific layer, not the shared `agent-runtime-base` that other G2
  AI-coding pipelines build on.

Both agentrunner pipelines (`environments/development/agent-runtime` for the
tools-server runners and `environments/development/agent-runtime-devserver` for
the dev-server ones) render the same `base/agent-runtime` manifests once per
workspace, via ArgoCD ApplicationSets in `g2crowd/configuration`
(`agentrunner-appset` / `agentrunner-dev-appset`). Those appsets patch the
`PLACEHOLDER` tokens in the Role/Policy/RolePolicyAttachment names — and in the
ServiceAccount's `role-arn` annotation — to the workspace slug, so every
workspace gets its own uniquely-named Role and Policy rather than sharing one.

Trust is `StringEquals` on both the namespace and ServiceAccount-name segments
of the `sub` claim (`system:serviceaccount:agentrunner-<slug>:agentrunner`,
or `agentrunner-dev-<slug>` for the dev-server pipeline) — exact, not a
wildcard, because each workspace now has its own Role to match against.

## What the role can reach

`s3:GetObject` and `s3:PutObject` on
`arn:aws:s3:::g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/*`,
plus `s3:ListBucket` on the bucket carrying an `s3:prefix` condition limited to
the same prefix. Nothing else — and nothing outside that one workspace's own
prefix, since each workspace has its own Role/Policy pair.

The prefix is the security boundary, and it is there because the backend writes
**user attachments** to the same bucket under `workspaces/<workspace-id>/...`.
Per-workspace scoping also means one workspace's agent sessions cannot read or
overwrite another workspace's scratch files — each workspace's Policy only
grants access to its own `agent-scratchpad/<slug>/` prefix.

The `s3:prefix` condition matters as much as the object-ARN scope: `ListObjectsV2`
is authorised against the *bucket* ARN, not the object ARN, so without it an
agent could enumerate every attachment key in the bucket even though it could not
read any of them.

`s3:DeleteObject` is deliberately absent — the request was read and upload.

Unchanged by any of this: the avatar-upload permissions in `iam-backend.yaml`
belong to `agentfarm-backend-role`, a different role.

### Using it from a session

```bash
aws s3 cp ./report.pdf s3://g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/report.pdf
aws s3 ls s3://g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/
aws s3 cp s3://g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/report.pdf ./report.pdf
```

Anything outside this workspace's own `agent-scratchpad/<workspace-slug>/`
returns `AccessDenied` — including another workspace's prefix and a bare
`aws s3 ls s3://g2-agentfarm-dev-uploads/`. That is the design, not a
misconfiguration.

## Why the CLI and not a FUSE mount

[Mountpoint for Amazon S3](https://github.com/awslabs/mountpoint-s3) would expose
the bucket as a directory, which is more ergonomic than teaching every agent an
`aws s3 cp` incantation. It was considered and not chosen, for three reasons.

**It is not a POSIX file system, and agents will assume it is.** On a
general-purpose bucket Mountpoint supports sequential writes to new files and
reads; it does *not* support random writes, appends, renames of any kind,
directory renames, hard links, symlinks, `chmod`/`chown`, or two writers on one
file. Overwrites need `--allow-overwrite` plus `O_TRUNC`; deletes need
`--allow-delete`. Upstream's own guidance is explicit — "don't work on your Git
repository or run `vim` in Mountpoint". An agent scratch directory is exactly
where a coding agent will run `git`, an editor, `sqlite`, or a build tool, and
each of those fails in a way that reads as a mysterious I/O error rather than
"this isn't a disk".

**Deploying it here costs cluster-level change we do not currently need.** FUSE
needs `/dev/fuse` and `CAP_SYS_ADMIN`; the runner container runs unprivileged
with `allowPrivilegeEscalation: false`, and granting `SYS_ADMIN` to a pod that
executes arbitrary agent-authored code is a real downgrade. The supported path is
the Mountpoint S3 CSI driver (EKS addon, Kubernetes v1.31+), which lives in
`g2crowd/configuration` rather than this repo, and which only does **static
provisioning** — a PersistentVolume per bucket/prefix plus a PVC in each
appset-generated `agentrunner-<slug>` namespace.

**It buys nothing the CLI does not already do** for the current use case, which
is "put a file somewhere durable, get it back later". Mountpoint's real strengths
are high-throughput sequential reads of large objects and letting tools that only
accept file paths read S3 data without staging it locally.

None of this is a permanent no. The IAM grant above is the same one Mountpoint
would need (it honours IAM and supports `--prefix`, so the per-workspace
`agent-scratchpad/<slug>/` boundary survives), so adopting it later is
additive — the CSI driver, a PV/PVC
per runner namespace, and `--allow-delete` / `--allow-overwrite` decisions. The
trigger to revisit would be a concrete case where an agent must stream a large
object it cannot afford to download, or hand a path to a tool that cannot speak
S3.

## Required follow-up: clear the agent's MCP config

**The IRSA role is inert until this is done.** Explicit `AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` in the MCP entry's `env` outrank
web identity in the AWS credential chain, and the daemon merges that config into
the environment the agent's tooling inherits — so the invalid keys would keep
winning, for the CLI too, not just for the MCP server.

Retiring the server and removing the bad credentials are the same action:

```bash
multica agent update <agent-id> --mcp-config null
```

If the agent has other MCP servers worth keeping, write the reduced config to a
file and pass `--mcp-config-file` instead — `mcp_config` is stored as secret
material and reads come back redacted, so re-supply every server you intend to
keep.

Worth checking at the same time: the agent's `custom_env`. Anything named
`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` there lands
in the pod environment and defeats IRSA in exactly the same way.

Pods must also be recreated once after the ServiceAccount annotation syncs — the
mutating pod-identity webhook only injects `AWS_ROLE_ARN` and
`AWS_WEB_IDENTITY_TOKEN_FILE` at pod creation — and must be running an image that
contains the CLI.

## Verifying

From an agent session on a runner pod, after the manifests have synced, the MCP
config has been cleared, and the pod has been recreated on a new image:

```bash
# Identity resolves at all — no InvalidClientTokenId.
aws sts get-caller-identity
# Expect an assumed-role ARN under agentfarm-agentrunner-role-<workspace-slug>
# (or -dev-<workspace-slug> on the dev-server pipeline).

# Object path: upload, list, read back.
echo hello > /tmp/probe.txt
aws s3 cp /tmp/probe.txt s3://g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/probe.txt
aws s3 ls s3://g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/
aws s3 cp s3://g2-agentfarm-dev-uploads/agent-scratchpad/<workspace-slug>/probe.txt -

# Boundary holds: all of these must fail with AccessDenied.
aws s3 ls s3://g2-agentfarm-dev-uploads/
aws s3 cp s3://g2-agentfarm-dev-uploads/workspaces/ . --recursive
aws s3 ls s3://g2-agentfarm-dev-uploads/agent-scratchpad/<other-workspace-slug>/
```
