# Eidetix provider-transport verification

Eidetix loads only if the provider backend supports a `url`-based (remote
HTTP/SSE) MCP server entry. Verify each backend a marketing agent will run on
BEFORE pointing real marketing work at it.

## Known status
- **Claude Code** — known-good (remote MCP via url/type).
- **OpenClaw** — known-good (partner demos `transport: streamable-http`).
- **Codex, Hermes, Gemini, kiro, kimi** — UNVERIFIED. A stdio-only backend will
  reject or ignore the entry. Do not assume graceful degradation — check.

## Procedure (per backend)
1. Bind a throwaway test project to a test Eidetix token:
   `printf '%s' "$TEST_TOKEN" | multica project eidetix set <project> --token-stdin --label Test`
2. Create an agent on the target provider; assign it an issue in that project.
3. In the agent run logs, confirm the `eidetix` MCP server initialized and that
   `recall`/`search` tools are listed as available.
4. Confirm the agent successfully calls `recall` (or that the tool is at least
   discoverable). A connection/tool-list error means the backend does not
   support the remote entry → mark UNSUPPORTED.
5. Record the result in the table below.

## Supported set (fill in during rollout)
| Backend | Remote MCP url entry loads? | Verified by | Date |
|---------|------------------------------|-------------|------|
| Claude Code | yes (known) | | |
| OpenClaw | yes (known) | | |
| Codex | ? | | |
| Hermes | ? | | |
| Gemini | ? | | |
| kiro | ? | | |
| kimi | ? | | |

## Operational rule
Marketing agents MUST run on a backend marked "yes". The per-project `enabled`
flag is the off switch if a binding misbehaves:
`multica project eidetix disable <project>`.
