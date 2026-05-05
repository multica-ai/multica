# Per-workspace resource templates

Files in this directory are **not** applied by Kustomize / ArgoCD. They
are the shape the `agentfarm-runtime-controller` renders at runtime
when standing up a per-workspace daemon (one set of resources per
workspace, named `agentfarm-daemon-<wsId>-…`). Placeholder
`WORKSPACE_ID` strings will never resolve in this form.

They live in Git so reviewers can audit what the controller produces
without having to read the controller's Go source. Per-workspace
resources themselves are intentionally never committed — see proposal
§"High-level shape".

## Files

- `externalsecret-per-workspace.yaml` — the ExternalSecret the
  controller stamps into each per-workspace `agentfarm-daemon-<wsId>-secrets`
  Secret. Pulls SSM keys under `/agentfarm/tools/daemons/<wsId>/*` and
  is augmented in-place by the controller with the workspace-scoped
  `MULTICA_TOKEN` and the LiteLLM-minted Anthropic / OpenAI virtual
  keys + base URLs.
