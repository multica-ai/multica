# Remove Multica from NanoClaw

Run from the NanoClaw project root for every configured group:

```bash
ncl groups config remove-mcp-server --id <group-id> --name multica
ncl groups restart --id <group-id>
```

Remove the copied MCP client:

```bash
rm container/agent-runner/src/multica-mcp-stdio.ts
```

Stop the host `multica nanoclaw serve` process or service. After every group is
unregistered, remove the limited bridge token:

```bash
rm ~/.config/multica-nanoclaw/bridge-token
```
