import assert from "node:assert/strict";
import { chmod, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

let handlers;
let tools;
let remote;
let observedAuthorization = "";
let mutationCalls = 0;
let resolvePolicy;

test.before(async () => {
	const command = join(tmpdir(), `fake-multica-${process.pid}`);
	await writeFile(command, `#!/usr/bin/env node
const lines = require("node:readline").createInterface({ input: process.stdin });
lines.on("line", line => {
  const request = JSON.parse(line);
  if (request.id === undefined) return;
  let result = {};
  if (request.method === "tools/list") result = { tools: [{ name: "connection_test", description: "Test a Multica Connection", inputSchema: { type: "object", properties: { value: { type: "string" } } } }] };
  if (request.method === "tools/call") result = { content: [{ type: "text", text: request.params.arguments.value }] };
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: request.id, result }) + "\\n");
});
`);
	await chmod(command, 0o700);
	remote = createServer((request, response) => {
		observedAuthorization = request.headers.authorization || "";
		let body = "";
		request.on("data", (chunk) => { body += chunk; });
		request.on("end", () => {
			const rpc = JSON.parse(body);
			let result = {};
			if (rpc.method === "tools/list") result = { tools: [
				{ name: "forecast", description: "Get forecast", inputSchema: { type: "object", properties: {} }, annotations: { readOnlyHint: true } },
				{ name: "charge", description: "Create a charge", inputSchema: { type: "object", properties: {} }, annotations: { readOnlyHint: false } },
			] };
			if (rpc.method === "tools/call" && rpc.params.name === "forecast") result = { content: [{ type: "text", text: "sunny" }] };
			if (rpc.method === "tools/call" && rpc.params.name === "charge") {
				mutationCalls++;
				request.socket.destroy();
				return;
			}
			response.setHeader("content-type", "application/json");
			response.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result }));
		});
	});
	await new Promise((resolve) => remote.listen(0, "127.0.0.1", resolve));
	const address = remote.address();
	const config = join(tmpdir(), `pi-harness-connections-${process.pid}.json`);
	await writeFile(config, JSON.stringify({ mcpServers: { weather: { type: "http", url: `http://127.0.0.1:${address.port}/mcp`, headers: { Authorization: "Bearer connection-secret" } } } }));
	process.env.MULTICA_PI_HARNESS_COMMAND = command;
	process.env.MULTICA_PI_HARNESS_MCP_CONFIG = config;
	process.env.CEREBRO_TOOLPOLICY_STAGE = "enforce";
	delete process.env.MULTICA_DAEMON_PORT;

	const harnessModule = await import(`./multica-harness.ts?test=${Date.now()}`);
	const harness = harnessModule.default;
	resolvePolicy = harnessModule.resolvePolicy;
	handlers = new Map();
	tools = [];
	harness({
		on(name, handler) { handlers.set(name, handler); },
		registerTool(tool) { tools.push(tool); },
	});
	await handlers.get("session_start")();
});

test.after(async () => {
	handlers.get("session_shutdown")();
	await new Promise((resolve) => remote.close(resolve));
});

test("D1 projects and calls REST API and MCP HTTP Connections", async () => {
	assert.deepEqual(tools.map((tool) => tool.name).sort(), ["connection_test", "mcp__weather__charge", "mcp__weather__forecast"]);
	const apiTool = tools.find((tool) => tool.name === "connection_test");
	assert.deepEqual(await apiTool.execute("call-1", { value: "ok" }), { content: [{ type: "text", text: "ok" }] });
	const mcpTool = tools.find((tool) => tool.name === "mcp__weather__forecast");
	assert.deepEqual(await mcpTool.execute("call-2", {}), { content: [{ type: "text", text: "sunny" }] });
	assert.equal(observedAuthorization, "Bearer connection-secret");
});

test("D2 fails tool policy closed in enforce mode", async () => {
	assert.deepEqual(await handlers.get("tool_call")({ toolName: "connection_test", input: {} }), {
		block: true,
		reason: "Multica tool policy is unavailable (failing closed)",
	});
});

test("D2 enforces Allow Ask and Deny decisions", async () => {
	const policy = createServer((request, response) => {
		let body = "";
		request.on("data", (chunk) => { body += chunk; });
		request.on("end", () => {
			const mode = JSON.parse(body).args.mode;
			const decision = mode === "deny"
				? { allowed: false, reason: "Denied by test policy" }
				: { allowed: true, reason: mode === "ask" ? "Approved after Ask" : "Allowed" };
			response.setHeader("content-type", "application/json");
			response.end(JSON.stringify(decision));
		});
	});
	await new Promise((resolve) => policy.listen(0, "127.0.0.1", resolve));
	try {
		const port = policy.address().port;
		assert.equal(await resolvePolicy("connection_test", { mode: "allow" }, { stage: "enforce", port }), undefined);
		assert.equal(await resolvePolicy("connection_test", { mode: "ask" }, { stage: "enforce", port }), undefined);
		assert.deepEqual(await resolvePolicy("connection_test", { mode: "deny" }, { stage: "enforce", port }), {
			block: true,
			reason: "Denied by test policy",
		});
	} finally {
		await new Promise((resolve) => policy.close(resolve));
	}
});

test("D4 does not retry an ambiguous mutation", async () => {
	const mutationTool = tools.find((tool) => tool.name === "mcp__weather__charge");
	const mutationResult = await mutationTool.execute("call-3", {});
	assert.equal(mutationCalls, 1);
	assert.equal(mutationResult.isError, true);
	assert.match(mutationResult.content[0].text, /outcome is unknown; not retried/i);
});

test("D6 loads through the exact Pi 0.80.7 extension loader", async () => {
	const piEntry = import.meta.resolve("@earendil-works/pi-coding-agent");
	const loader = await import(new URL("./core/extensions/loader.js", piEntry));
	const extensionPath = fileURLToPath(new URL("./multica-harness.ts", import.meta.url));
	const loaded = await loader.loadExtensions([extensionPath], process.cwd());
	assert.deepEqual(loaded.errors, []);
	assert.equal(loaded.extensions.length, 1);
});
