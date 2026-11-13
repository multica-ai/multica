Protocol evidence for the Droid adapter

See the [capability audit](droid-capability-audit.md) for the full official-documentation checklist, native argument policy, configuration boundaries, and remaining acceptance checks.

- Official native macOS arm64 Droid 1.8.0, downloaded from Factory's release endpoint. SHA-256: `c8dd0da4d01cc8c08f4f441c183a22bcf713bef17e82d8ba7a77ffd7899d06bc`, matched the vendor checksum.
- `droid exec --help` verified stdin prompt input, stream-json output, model selection, session continuation, and autonomy flags.
- An unauthenticated read-only invocation emitted `system/init`, then `error` with `source: "cli"` and `message: "Error: Authentication failed. Please log in using /login or set a valid FACTORY_API_KEY environment variable."`, and exited 1.
- `droid-1.8.0-protocol-example.jsonl` is a **synthetic example**, not a successful account-backed capture. Its field names were checked against the stream-json serializer in the official executable and the [official headless documentation](https://docs.factory.com/droid-exec/overview). In particular, tool events use `id`, `toolName`, `parameters`, `value`, `isError`; completion uses `finalText` and does not carry an error flag. Earlier `error` events must remain failures.
- Successful account-backed execution, resume, and real tool execution remain unverified without a Factory credential. The opt-in `TestDroidRealTaskSmoke` reads a file using a tool, checks the response, then resumes the same session to verify memory. Its local attempt failed at authentication before tool execution. `TestDroidRealInvalidAuthentication` passed through the adapter with the official binary, verifying the failed status and an empty final output.
- The daemon leaves `SystemPrompt` empty for Droid and writes its runtime brief to task `AGENTS.md`. The adapter's optional system-prompt file support is not used by daemon task dispatch. Native instruction discovery is still unverified: opt-in `TestDroidRealAgentsMDCanary` places a random marker only in `AGENTS.md`, passes no inline instructions or marker in the turn prompt, and requires an exact response without any tool calls. It has not been run with a Factory account.

Run only with an explicitly authorized test account:

```sh
MULTICA_RUN_REAL_AGENT_SMOKE=1 MULTICA_DROID_PATH=/path/to/droid go test -tags=agentintegration ./pkg/agent -run '^TestDroidReal(AgentsMDCanary|TaskSmoke)$' -count=1 -v
```

These tests access the real CLI only after the `agentintegration` build tag and `MULTICA_RUN_REAL_AGENT_SMOKE=1` gate. Default tests use the test executable as a fake CLI and have no Factory account dependency.
