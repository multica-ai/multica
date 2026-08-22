// The metrics MCP server behind the `metrics` hook.
//
// This is the case the `mcp` transport exists for: the team already runs an MCP
// server and does not want to re-expose it as HTTP hooks. Multica adopts its
// tools — but only the ones an administrator approved, pinned by schema digest.
//
//   node server/metrics-mcp.mjs
//
// Env: METRICS_TOKEN (matched against the `metrics_credential` plugin secret)
//      PORT (default 8789)
//
// Try adding a tool below and re-opening the approval panel: the new tool shows
// up as unapproved and agents cannot call it until somebody ticks it.

import { createServer } from "node:https";
import { readFileSync } from "node:fs";

// HTTPS, not HTTP. A hook's transport URL must be an https:// URL or the
// manifest will not install, and MULTICA_PLUGIN_DEV_CA only changes WHICH
// certificate Multica trusts — it never turns verification off. Generate one:
//
//   openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
//     -keyout dev-key.pem -out dev-cert.pem \
//     -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1"
//
// then point MULTICA_PLUGIN_DEV_CA at dev-cert.pem.
function tlsOptions() {
  const cert = process.env.TLS_CERT ?? "dev-cert.pem";
  const key = process.env.TLS_KEY ?? "dev-key.pem";
  try {
    return { cert: readFileSync(cert), key: readFileSync(key) };
  } catch (error) {
    console.error(`Could not read ${cert} / ${key}: ${error.message}`);
    console.error("See the README for the openssl one-liner that generates them.");
    process.exit(1);
  }
}

const PORT = Number(process.env.PORT ?? 8789);
const TOKEN = process.env.METRICS_TOKEN ?? "metrics-dev-token";

const TOOLS = [
  {
    name: "query_timeseries",
    description: "Query a metric over a time window. Returns datapoints at the coarsest resolution that covers the window.",
    inputSchema: {
      type: "object",
      required: ["metric", "window_minutes"],
      properties: {
        metric: { type: "string", description: "e.g. checkout-api.error_rate" },
        window_minutes: { type: "number" },
      },
    },
  },
  {
    name: "list_alerts",
    description: "Alerts that fired in a window, most recent first, with the threshold each one crossed.",
    inputSchema: {
      type: "object",
      properties: { window_minutes: { type: "number" }, service: { type: "string" } },
    },
  },
  {
    name: "service_dependencies",
    description: "Which services this one calls and which call it. Useful for deciding whether an error is originating here or arriving from upstream.",
    inputSchema: {
      type: "object",
      required: ["service"],
      properties: { service: { type: "string" } },
    },
  },
];

const SERIES = {
  "checkout-api.error_rate": [
    { minutes_ago: 6, value: 0.002 }, { minutes_ago: 4, value: 0.058 },
    { minutes_ago: 2, value: 0.061 }, { minutes_ago: 0, value: 0.064 },
  ],
};

function callTool(name, args) {
  switch (name) {
    case "query_timeseries": {
      const points = SERIES[args.metric] ?? [];
      const within = points.filter((point) => point.minutes_ago <= Number(args.window_minutes ?? 60));
      return within.length === 0
        ? `No datapoints for ${args.metric} in the last ${args.window_minutes} minutes.`
        : `${args.metric}: ${within.map((p) => `${p.minutes_ago}m ago = ${p.value}`).join(", ")}`;
    }
    case "list_alerts":
      return "ALERT checkout-api.error_rate crossed 0.05 (fired 4m ago, still firing).";
    case "service_dependencies":
      return `${args.service} calls: payments-api, inventory-api. Called by: web, mobile-bff.`;
    default:
      return null;
  }
}

createServer(tlsOptions(), async (req, res) => {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);

  res.setHeader("Content-Type", "application/json");
  if (req.headers.authorization !== `Bearer ${TOKEN}`) {
    res.writeHead(401);
    return res.end(JSON.stringify({ error: "credential rejected" }));
  }

  let request;
  try {
    request = JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
  } catch {
    res.writeHead(400);
    return res.end();
  }

  const send = (result) => res.end(JSON.stringify({ jsonrpc: "2.0", id: request.id, result }));

  switch (request.method) {
    case "initialize":
      return send({
        protocolVersion: "2025-03-26",
        capabilities: { tools: {} },
        serverInfo: { name: "deploy-sentinel-metrics", version: "1.0.0" },
      });
    case "notifications/initialized":
      res.writeHead(202);
      return res.end();
    case "tools/list":
      return send({ tools: TOOLS });
    case "tools/call": {
      const text = callTool(request.params?.name, request.params?.arguments ?? {});
      if (text === null) {
        return res.end(JSON.stringify({
          jsonrpc: "2.0", id: request.id,
          error: { code: -32602, message: `unknown tool ${request.params?.name}` },
        }));
      }
      return send({ content: [{ type: "text", text }] });
    }
    default:
      return res.end(JSON.stringify({
        jsonrpc: "2.0", id: request.id,
        error: { code: -32601, message: `method not found: ${request.method}` },
      }));
  }
}).listen(PORT, () => console.log(`metrics MCP server on https://127.0.0.1:${PORT}`));
