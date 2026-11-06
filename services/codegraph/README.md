# CodeGraph engine

An optional, private sidecar for repository indexing and read-only queries.
The Go API clones Git repositories, prepares Markdown/YAML document indexes, and
publishes immutable snapshots through this engine. Agents receive query tools
only for repositories assigned to them.

## Run locally

From the repository root:

```sh
docker build -f services/codegraph/Dockerfile -t multica-codegraph:local .
mkdir -p /tmp/multica-codegraphs /tmp/multica-codegraph-builds
docker run --rm --name multica-codegraph \
  -p 127.0.0.1:8787:8787 \
  -e MULTICA_CODEGRAPH_ROOT=/tmp/multica-codegraphs \
  -e MULTICA_CODEGRAPH_WORK_ROOT=/tmp/multica-codegraph-builds \
  -v /tmp/multica-codegraphs:/tmp/multica-codegraphs \
  -v /tmp/multica-codegraph-builds:/tmp/multica-codegraph-builds \
  multica-codegraph:local
```

Set these values in the API's managed environment, then restart the API:

```dotenv
MULTICA_CODEGRAPH_ENGINE_URL=http://127.0.0.1:8787
MULTICA_CODEGRAPH_ROOT=/tmp/multica-codegraphs
MULTICA_CODEGRAPH_WORK_ROOT=/tmp/multica-codegraph-builds
```

The API and sidecar must see both roots at the **same absolute paths**.
The roots must differ from each other. Build scratch storage can be fast,
ephemeral storage; the snapshot root must be persistent in deployments.
The API requires Git on PATH. The engine is Linux based.

Do not expose the engine port publicly. It is a trusted internal service without
end-user authentication; all authorization belongs to the API.

## Resource settings

| Variable | Process | Default | Accepted values |
| --- | --- | --- | --- |
| `MULTICA_CODEGRAPH_BUILD_TIMEOUT_SECONDS` | API and engine | 660 | 120–86400 seconds |
| `MULTICA_CODEGRAPH_HEAP_MB` | Engine | 768 | 256–8192 MiB |
| `MULTICA_CODEGRAPH_COPY_CONCURRENCY` | Engine | 4 | 1–16 |
| `MULTICA_CODEGRAPH_HOST` | Engine | 0.0.0.0 | Listen address |

Use the same timeout on the API and engine. Queue waiting does not consume the
active build budget. Git fetch preparation can overlap, but graph extraction is
limited to one build at a time to bound memory. Publishing validates a staging
snapshot before making it active. A failed rebuild preserves the last successful
snapshot. No automatic retry is performed after an interrupted build.

Private HTTPS repositories require an explicitly selected workspace Git
credential. Configure the existing `MULTICA_VCS_SECRET_KEY` on the API to encrypt
tokens at rest. Keep the same key across replicas and back it up separately from
the database. Losing the key requires re-entering credentials. The engine never
receives Git tokens.

## Third-party source

The source-code extraction and graph-query engine is
[CodeGraph by Colby Mchenry](https://github.com/colbymchenry/codegraph).
The adapter directly uses its `CodeGraph` library for indexing, symbol search
and call relationships. Credit for that engine belongs to its original authors.

The image builds the fixed
[19813544766f74a10f403c9e474306c0f7b61a18 revision](https://github.com/colbymchenry/codegraph/tree/19813544766f74a10f403c9e474306c0f7b61a18),
recorded in `source-revision.txt`, rather than following a moving branch.
That revision is [MIT licensed](https://github.com/colbymchenry/codegraph/blob/19813544766f74a10f403c9e474306c0f7b61a18/LICENSE),
copyright (c) 2026 Colby Mchenry. The original license remains at
`/opt/codegraph/LICENSE` in the image; the full notice is also retained in
`THIRD_PARTY_NOTICES.md`.

This contribution adds Multica workspace authorization, Git credential
management, build scheduling/cancellation, validated snapshot publication,
Markdown/YAML document structure, task-authenticated MCP delivery, and shared
web/desktop configuration screens around that engine. It exposes a subset of
the pinned library's APIs; it does not imply support for every feature in the
original project's current CLI or main branch.

All queries require workspace membership or an authorized agent task token.
Agent authorization combines direct assignments, workspace sharing, and the
authenticated task's current project assignments. New scopes default off.
Agent owners can manage direct assignments to their agents; only workspace
owners and administrators can change project and workspace scopes. The server
derives the project from database task relationships and rechecks grants for
every tool call, including file reads. Revocation cannot erase code already in
the agent's conversation. Graph assignment is separate from runtime tool support.
Anonymous Git fetching is available for public repositories; anonymous graph
querying and Skill export are not included.
