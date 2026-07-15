// @ts-nocheck -- Pi loads this dependency-free extension directly; behavior is covered by the protocol integration test.
import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { createInterface } from "node:readline";

const command = process.env.MULTICA_PI_HARNESS_COMMAND || "multica";
const policyStage = process.env.CEREBRO_TOOLPOLICY_STAGE || "off";
const policyPort = process.env.MULTICA_DAEMON_PORT || "";
const connectionsConfig = process.env.MULTICA_PI_HARNESS_MCP_CONFIG || "";

class McpClient {
	constructor() {
		this.nextId = 1;
		this.pending = new Map();
		this.process = undefined;
	}

	async start() {
		if (this.process) return;
		const child = spawn(command, ["mcp", "serve"], {
			stdio: ["pipe", "pipe", "inherit"],
			env: process.env,
		});
		this.process = child;
		createInterface({ input: child.stdout }).on("line", (line) => this.receive(line));
		child.once("exit", (code) => this.failAll(new Error(`Multica MCP exited with code ${code}`)));
		child.once("error", (error) => this.failAll(error));
		await this.request("initialize", {
			protocolVersion: "2024-11-05",
			capabilities: {},
			clientInfo: { name: "firtal-pi-harness", version: "1" },
		});
		this.notify("notifications/initialized", {});
	}

	receive(line) {
		let message;
		try {
			message = JSON.parse(line);
		} catch {
			return;
		}
		if (message.id === undefined) return;
		const pending = this.pending.get(String(message.id));
		if (!pending) return;
		this.pending.delete(String(message.id));
		if (message.error) pending.reject(new Error(message.error.message || "MCP request failed"));
		else pending.resolve(message.result);
	}

	request(method, params) {
		if (!this.process?.stdin.writable) return Promise.reject(new Error("Multica MCP is unavailable"));
		const id = this.nextId++;
		return new Promise((resolve, reject) => {
			this.pending.set(String(id), { resolve, reject });
			this.process.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`);
		});
	}

	notify(method, params) {
		this.process?.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", method, params })}\n`);
	}

	failAll(error) {
		this.process = undefined;
		for (const pending of this.pending.values()) pending.reject(error);
		this.pending.clear();
	}

	close() {
		this.process?.stdin.end();
		this.process?.kill();
		this.process = undefined;
	}
}

class HttpMcpClient {
	constructor(url, headers) {
		this.url = url;
		this.headers = headers || {};
		this.nextId = 1;
		this.sessionId = "";
	}

	async start() {
		await this.request("initialize", {
			protocolVersion: "2024-11-05",
			capabilities: {},
			clientInfo: { name: "firtal-pi-harness", version: "1" },
		});
		await this.notify("notifications/initialized", {});
	}

	async exchange(message) {
		const headers = {
			...this.headers,
			"content-type": "application/json",
			accept: "application/json, text/event-stream",
		};
		if (this.sessionId) headers["mcp-session-id"] = this.sessionId;
		const response = await fetch(this.url, { method: "POST", headers, body: JSON.stringify(message) });
		if (!response.ok) throw new Error(`MCP HTTP returned ${response.status}`);
		this.sessionId = response.headers.get("mcp-session-id") || this.sessionId;
		if (response.status === 202) return undefined;
		const text = await response.text();
		if (!text.trim()) return undefined;
		if ((response.headers.get("content-type") || "").includes("text/event-stream")) {
			const data = text.split(/\r?\n/).find((line) => line.startsWith("data:"));
			return data ? JSON.parse(data.slice(5).trim()) : undefined;
		}
		return JSON.parse(text);
	}

	async request(method, params) {
		const id = this.nextId++;
		const response = await this.exchange({ jsonrpc: "2.0", id, method, params });
		if (response?.error) throw new Error(response.error.message || "MCP request failed");
		return response?.result;
	}

	async notify(method, params) {
		await this.exchange({ jsonrpc: "2.0", method, params });
	}

	close() {}
}

async function configuredHttpClients() {
	if (!connectionsConfig) return [];
	const document = JSON.parse(await readFile(connectionsConfig, "utf8"));
	const clients = [];
	for (const [name, config] of Object.entries(document.mcpServers || {})) {
		if (!config || typeof config !== "object" || typeof config.url !== "string") continue;
		const client = new HttpMcpClient(config.url, config.headers);
		await client.start();
		clients.push({ name, client });
	}
	return clients;
}

function textResult(error, ambiguousMutation = false) {
	const message = error instanceof Error ? error.message : String(error);
	const text = ambiguousMutation ? `Mutation transport failed; outcome is unknown; not retried. ${message}` : message;
	return { content: [{ type: "text", text }], isError: true };
}

export async function resolvePolicy(toolName, input, override = {}) {
	const stage = override.stage || policyStage;
	const port = override.port || policyPort;
	if (stage === "off") return undefined;
	if (!port) {
		return stage === "enforce"
			? { block: true, reason: "Multica tool policy is unavailable (failing closed)" }
			: undefined;
	}
	try {
		const response = await fetch(`http://127.0.0.1:${port}/tool-policy/resolve`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({
				workspace_id: process.env.MULTICA_WORKSPACE_ID || "",
				agent_id: process.env.MULTICA_AGENT_ID || "",
				tool_name: toolName,
				resource_pattern: "",
				args: input || {},
				stage,
			}),
		});
		if (!response.ok) throw new Error(`tool policy returned HTTP ${response.status}`);
		const decision = await response.json();
		if (!decision.allowed) return { block: true, reason: decision.reason || "Blocked by Multica tool policy" };
		return undefined;
	} catch (error) {
		if (stage === "enforce") {
			return { block: true, reason: "Multica tool policy is unavailable (failing closed)" };
		}
		return undefined;
	}
}

export default function multicaHarness(pi) {
	const client = new McpClient();
	const clients = [client];
	const registered = new Set();
	const register = (tool, source, exposedName) => {
		if (!tool?.name || registered.has(exposedName)) return;
		registered.add(exposedName);
		pi.registerTool({
			name: exposedName,
			label: tool.title || exposedName,
			description: tool.description || `Call Multica tool ${exposedName}`,
			promptSnippet: tool.description || `Call Multica tool ${exposedName}`,
			parameters: tool.inputSchema || { type: "object", properties: {} },
			async execute(_toolCallId, params) {
				try {
					return await source.request("tools/call", { name: tool.name, arguments: params || {} });
				} catch (error) {
					return textResult(error, tool.annotations?.readOnlyHint === false);
				}
			},
		});
	};

	pi.on("session_start", async () => {
		await client.start();
		const listed = await client.request("tools/list", {});
		for (const tool of listed.tools || []) register(tool, client, tool.name);
		for (const remote of await configuredHttpClients()) {
			clients.push(remote.client);
			const remoteTools = await remote.client.request("tools/list", {});
			for (const tool of remoteTools.tools || []) {
				register(tool, remote.client, `mcp__${remote.name}__${tool.name}`);
			}
		}
	});

	pi.on("tool_call", (event) => resolvePolicy(event.toolName, event.input));
	pi.on("session_shutdown", () => clients.forEach((entry) => entry.close()));
}
