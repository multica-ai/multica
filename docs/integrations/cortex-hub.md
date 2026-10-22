# Cortex Hub MCP integration

This is an opt-in, daemon-local integration for shared memory and code intelligence. GitHub Issues and commits remain the source of truth; Cortex stores only selected derived context with trace references. Cortex Conductor is not part of this integration.

## Architecture

```text
Multica daemon (local)
  ├─ reads MULTICA_CORTEX_MCP_URL + MULTICA_CORTEX_MCP_API_KEY
  ├─ injects a redacted `cortex-hub` MCP entry into supported agent launches
  └─ never persists or sends the key through the Multica server
          │ Streamable HTTP + Bearer key
          ▼
Cortex Hub MCP → Dashboard API → Qdrant / GitNexus
          ▲
          └─ derived memory/knowledge includes repo + issue + commit references
```

The daemon-owned entry wins if an agent-local config uses the same `cortex-hub` name. This prevents an old local entry from silently replacing the deployment key or endpoint. Existing provider-native MCP configuration remains unchanged when the environment variables are absent.

The daemon currently injects the entry for the providers whose runtime MCP config is merged locally: Claude, CodeArts, Codex, Cursor, OpenCode, and OpenClaw. Other providers can use the same Cortex endpoint through their native MCP configuration until their runtime adapter is explicitly added.

## Cortex key policy

Create a dedicated key for this integration with only the capabilities needed by the selected agents. A conservative shared-memory/code-intelligence set is:

```text
cortex.session.start
cortex.session.end
cortex.memory.store
cortex.memory.search
cortex.knowledge.store
cortex.knowledge.search
cortex.code.search
cortex.code.context
cortex.code.impact
cortex.list.repos
cortex.cypher
cortex.detect.changes
cortex.health
```

Do not grant `cortex.code.reindex` to coding agents by default. Do not grant any `cortex.task.*`, `cortex.task.tree.create`, `cortex.agents`, `cortex.task.review`, `cortex.task.route`, or `cortex.task.cancel` permission: those belong to Conductor/work orchestration and are deliberately outside this POC. The key's human-readable scope is metadata; the permission list is the effective MCP tool gate.

## Configuration

The `fz-tools` pilot endpoint is `https://multica-api.home.arpa/mcp`. Store the daemon variables in Infisical project `550dfa45-1664-435b-bd65-2fe8164d80db`, environment `dev`, path `/fztools/multica-cortex`. Keep the Cortex-only `INTERNAL_API_SECRET` separately under `/fztools/cortex-hub`; do not materialize the MCP key as a Kubernetes Secret or inject it into the Multica backend.

On the machine that runs the Multica daemon, start/restart that same daemon with:

```text
MULTICA_CORTEX_MCP_URL=https://multica-api.home.arpa/mcp
MULTICA_CORTEX_MCP_API_KEY=<dedicated-Cortex-key-from-Infisical>
```

For a CLI-managed daemon, Infisical can inject the path directly into its process environment without writing a dotenv file:

```sh
infisical --domain http://infisical.home.arpa/api run \
  --projectId 550dfa45-1664-435b-bd65-2fe8164d80db \
  --env dev --path /fztools/multica-cortex -- multica daemon
```

The daemon normalizes the endpoint to `/mcp`, requires an absolute `http` or `https` URL, rejects URL credentials/query/fragment values, and rejects keys containing line breaks. Do not put the key in the Multica web server, database, issue body, repository, or agent capability inventory.

On the Cortex deployment, set a random `INTERNAL_API_SECRET` in both `cortex-api` and `cortex-mcp` if dashboard key revocation/cache invalidation is enabled. Set `MCP_ALLOWED_ORIGINS` to exact browser origins, comma-separated. Terminal MCP clients normally omit `Origin` and remain supported; browser-originated calls fail closed when no allowlist is configured.

## Selective extraction contract

GitHub remains authoritative. A coding agent may write to Cortex only after it has selected a reusable fact, decision, resolved failure, or code-navigation insight from an issue/commit. The `fz-tools` POC sets `REQUIRE_SOURCE_TRACE=true`, so Cortex rejects each memory/knowledge write unless it includes `sourceRefs.issue` or `sourceRefs.commit`; include the repository when available. A write should carry:

```json
{
  "sourceRefs": {
    "repo": "https://github.com/example/multica",
    "issue": "https://github.com/example/multica/issues/123",
    "commit": "0123456789abcdef0123456789abcdef01234567"
  }
}
```

`cortex_knowledge_store` appends the references to the stored document and adds searchable trace tags. `cortex_memory_store` appends the references to the memory and records them in `metadata.source_refs`. Do not store credentials, tokens, private customer data, or an entire issue thread. If the source issue changes, update or delete the derived entry rather than treating Cortex as a second source of truth.

## Connection check

1. Run the read-only benchmark with the Infisical-provided key; it checks `tools/list` without printing the bearer token:

   ```sh
   node scripts/benchmark-cortex-mcp.mjs
   ```

2. Start one supported Multica agent with the two environment variables set.
3. Confirm the agent sees `cortex-hub` in its MCP tools and can call `cortex_health`.
4. Perform one read-only `cortex_memory_search` or `cortex_code_search` call.
5. Verify the Multica daemon capability inventory contains only the name, transport, source, and enabled state—not the URL, Authorization header, or key.

No Conductor task tree, routing, or agent delegation should appear in this test. This integration does not add or start a separate Conductor worker; existing platform routes remain untouched.

## Benchmark: token and cache overhead

The repository includes a read-only MCP benchmark:

```sh
MULTICA_CORTEX_MCP_URL=https://mcp.example.com/mcp \
MULTICA_CORTEX_MCP_API_KEY='<key>' \
node scripts/benchmark-cortex-mcp.mjs
```

It warms the MCP authentication path, repeats `tools/list` by default, and reports first-call versus warm-cache latency, p50/p95 latency, status, and response bytes. To benchmark memory retrieval instead:

```sh
CORTEX_BENCHMARK_METHOD=cortex_memory_search \
CORTEX_BENCHMARK_ARGS='{"query":"Multica deployment decision","limit":3}' \
CORTEX_BENCHMARK_REPEATS=5 \
node scripts/benchmark-cortex-mcp.mjs
```

Run the benchmark before and after enabling the Multica environment variables. For model-token impact, compare equivalent agent turns in the Multica telemetry/usage view and record prompt/output tokens separately from MCP response bytes; MCP transport bytes are not model tokens. Keep the same repository, prompt, provider, model, and number of recalled results in both runs. A POC pass requires no unexpected secret-bearing logs, stable permission-denied behavior for Conductor tools, and no unbounded growth in returned context.

## Rollback

Rollback is configuration-only:

1. Remove `MULTICA_CORTEX_MCP_URL` and `MULTICA_CORTEX_MCP_API_KEY` from the local daemon environment (or the `/fztools/multica-cortex` Infisical path).
2. Restart the Multica daemon or the affected runtime process.
3. Confirm the agent starts with its previous native MCP configuration and the `cortex-hub` inventory entry disappears.
4. If Cortex data must be removed, delete only the derived memory/knowledge records by their returned IDs; do not delete GitHub Issues or commits.

No Multica database migration is required. Reverting the daemon build is safe because the absent environment variables preserve the previous inheritance path. For a Cortex-only rollback, redeploy the previous MCP image and unset `MCP_ALLOWED_ORIGINS` only if the previous behavior is intentionally required; retain the API key and internal-secret rotation records separately.
