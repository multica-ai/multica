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

The image builds [CodeGraph](https://github.com/colbymchenry/codegraph) at the
revision pinned in `source-revision.txt`. That project is MIT licensed,
copyright (c) 2026 Colby Mchenry. Its original license remains at
`/opt/codegraph/LICENSE` in the image. See `THIRD_PARTY_NOTICES.md`.

This integration does not provide Skill export.
