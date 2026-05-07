// CLI entry point. MCP clients spawn this binary with stdio inherited
// and speak JSON-RPC over the pipe. Anything we log to stdout would
// corrupt the protocol — every message goes to stderr instead.

import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import { MulticaClient } from "./client.js";
import { ConfigError, loadConfig } from "./config.js";
import { createServer } from "./server.js";

async function main(): Promise<void> {
  let config;
  try {
    config = loadConfig();
  } catch (err) {
    if (err instanceof ConfigError) {
      process.stderr.write(`multica-mcp: ${err.message}\n`);
      process.exit(2);
    }
    throw err;
  }

  const client = new MulticaClient(config);
  const { server, tools } = createServer({ client });

  process.stderr.write(
    `multica-mcp: starting (api=${config.apiUrl}, workspace=${config.defaultWorkspaceId ?? "<unset>"}, tools=${tools.length})\n`,
  );

  const transport = new StdioServerTransport();
  await server.connect(transport);

  // Stay alive until the parent closes stdio. The transport handles the
  // shutdown signal; we just keep the event loop pinned.
  process.on("SIGINT", () => process.exit(0));
  process.on("SIGTERM", () => process.exit(0));
}

main().catch((err) => {
  process.stderr.write(`multica-mcp: fatal: ${err instanceof Error ? err.stack ?? err.message : String(err)}\n`);
  process.exit(1);
});
