import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { spawn } from "node:child_process";

const root = dirname(fileURLToPath(import.meta.url));
const configPath = join(root, "..", "mcp.json");
const toolSchema = { type: "object", additionalProperties: true };

function serverEntries(config) {
  const servers = config?.mcpServers ?? config?.mcp ?? {};
  return Object.entries(servers).filter(([, server]) => server && typeof server === "object");
}

function toolName(server, tool) {
  return `mcp__${server}__${tool}`.replace(/[^a-zA-Z0-9_]/g, "_").slice(0, 128);
}

function resultText(result) {
  const content = result?.content ?? [];
  return content.map((item) => item.type === "text" ? item.text : JSON.stringify(item)).join("\n") || JSON.stringify(result ?? {});
}

class StdioClient {
  constructor(server) {
    this.server = server;
    this.id = 0;
    this.pending = new Map();
    this.buffer = Buffer.alloc(0);
    const env = { ...process.env, ...(server.env ?? {}) };
    this.child = spawn(server.command, server.args ?? [], { cwd: server.cwd, env, stdio: ["pipe", "pipe", "pipe"] });
    this.child.stdout.on("data", (chunk) => this.onData(chunk));
    this.child.on("close", () => this.fail(new Error("MCP server exited")));
    this.child.on("error", (error) => this.fail(error));
  }

  fail(error) {
    for (const pending of this.pending.values()) pending.reject(error);
    this.pending.clear();
  }

  onData(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      const lineEnd = this.buffer.indexOf("\n");
      if (headerEnd >= 0) {
        const headers = this.buffer.subarray(0, headerEnd).toString();
        const match = headers.match(/Content-Length:\s*(\d+)/i);
        if (!match) break;
        const length = Number(match[1]);
        const start = headerEnd + 4;
        if (this.buffer.length < start + length) break;
        this.resolve(JSON.parse(this.buffer.subarray(start, start + length).toString()));
        this.buffer = this.buffer.subarray(start + length);
      } else if (lineEnd >= 0) {
        const line = this.buffer.subarray(0, lineEnd).toString().trim();
        this.buffer = this.buffer.subarray(lineEnd + 1);
        if (line) this.resolve(JSON.parse(line));
      } else break;
    }
  }

  resolve(message) {
    if (message.id == null) return;
    const pending = this.pending.get(message.id);
    if (!pending) return;
    this.pending.delete(message.id);
    if (message.error) pending.reject(new Error(message.error.message ?? "MCP request failed"));
    else pending.resolve(message.result);
  }

  notify(method, params = {}) {
    const body = JSON.stringify({ jsonrpc: "2.0", method, params });
    this.child.stdin.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
  }

  request(method, params = {}) {
    const id = ++this.id;
    const body = JSON.stringify({ jsonrpc: "2.0", id, method, params });
    this.child.stdin.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
  }

  close() { this.child.kill(); }
}

class HttpClient {
  constructor(server) { this.server = server; this.sessionId = undefined; }
  async request(method, params = {}) {
    const headers = { Accept: "application/json, text/event-stream", "Content-Type": "application/json", ...(this.server.headers ?? {}) };
    if (this.sessionId) headers["Mcp-Session-Id"] = this.sessionId;
    const response = await fetch(this.server.url, { method: "POST", headers, body: JSON.stringify({ jsonrpc: "2.0", id: Date.now(), method, params }) });
    if (!response.ok) throw new Error(`MCP HTTP ${response.status}`);
    const sessionId = response.headers.get("mcp-session-id");
    if (sessionId) this.sessionId = sessionId;
    const text = await response.text();
    const data = text.split("\n").filter((line) => line.startsWith("data:")).map((line) => line.slice(5).trim()).pop() ?? text;
    const result = JSON.parse(data);
    if (result.error) throw new Error(result.error.message ?? "MCP request failed");
    return result.result;
  }
  close() {}
}

async function connect(server) {
  const client = server.command ? new StdioClient(server) : new HttpClient(server);
  await client.request("initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "multica-pi", version: "1.0.0" } });
  if (client.notify) client.notify("notifications/initialized", {});
  const listed = await client.request("tools/list", {});
  return { client, tools: listed?.tools ?? [] };
}

export default function multicaMcpExtension(pi) {
  const clients = new Map();
  const registered = new Set();

  const load = async () => {
    const config = JSON.parse(await readFile(configPath, "utf8"));
    for (const [serverName, server] of serverEntries(config)) {
      try {
        const connection = await connect(server);
        clients.set(serverName, connection);
        for (const tool of connection.tools) {
          const name = toolName(serverName, tool.name);
          if (registered.has(name)) continue;
          registered.add(name);
          pi.registerTool({
            name,
            label: `${serverName}: ${tool.name}`,
            description: tool.description ?? `MCP tool ${tool.name} from ${serverName}`,
            parameters: tool.inputSchema ?? toolSchema,
            async execute(_id, params) {
              const current = clients.get(serverName);
              if (!current) throw new Error(`MCP server ${serverName} is unavailable`);
              const result = await current.client.request("tools/call", { name: tool.name, arguments: params });
              return { content: [{ type: "text", text: resultText(result) }], details: result };
            },
          });
        }
      } catch (error) {
        console.error(`[multica-mcp] failed to connect ${serverName}: ${error.message}`);
      }
    }
  };

  pi.on("session_start", load);
  pi.on("session_shutdown", async () => {
    for (const connection of clients.values()) connection.client.close();
  });
}
