#!/usr/bin/env node
/**
 * Schedule Pulse handler — the plugin author's side of a durable scheduled Hook.
 *
 * Run:
 *   MULTICA_SIGNING_SECRET=whsec_... node handler.mjs
 */

import { createServer as createHTTPServer } from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import { createHmac, timingSafeEqual } from "node:crypto";
import { readFileSync } from "node:fs";

const PORT = Number(process.env.PORT ?? 8787);
const SIGNING_SECRET = process.env.MULTICA_SIGNING_SECRET ?? "";
const TOLERANCE_SECONDS = 5 * 60;
const PULSE_KEY = "last_pulse";

if (!SIGNING_SECRET) {
  console.error("MULTICA_SIGNING_SECRET is required. Rotate the plugin token in Multica to obtain it.");
  process.exit(1);
}

const seenSignatures = new Map();

function rememberSignature(signature, now) {
  for (const [seen, at] of seenSignatures) {
    if (now - at > TOLERANCE_SECONDS * 1000) seenSignatures.delete(seen);
  }
  if (seenSignatures.has(signature)) return false;
  seenSignatures.set(signature, now);
  return true;
}

function verify(rawBody, headers) {
  const timestamp = headers["x-multica-timestamp"];
  const presented = String(headers["x-multica-signature"] ?? "").replace(/^v1=/, "");
  if (!timestamp || !presented) return "missing signature headers";

  const drift = Math.abs(Math.floor(Date.now() / 1000) - Number(timestamp));
  if (!Number.isFinite(drift) || drift > TOLERANCE_SECONDS) return "timestamp outside the accepted window";

  const secret = Buffer.from(SIGNING_SECRET.replace(/^whsec_/, ""), "hex");
  const expected = createHmac("sha256", secret)
    .update(timestamp)
    .update(".")
    .update(rawBody)
    .digest("hex");

  const expectedBytes = Buffer.from(expected, "utf8");
  const presentedBytes = Buffer.from(presented, "utf8");
  if (expectedBytes.length !== presentedBytes.length) return "signature does not match";
  if (!timingSafeEqual(expectedBytes, presentedBytes)) return "signature does not match";
  if (!rememberSignature(presented, Date.now())) return "this request was already delivered";
  return null;
}

async function callback(body, method, path, payload) {
  const response = await fetch(`${body.callback_url}${path}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${body.callback_token}`,
    },
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  const text = await response.text();
  if (response.status === 404) return null;
  if (!response.ok) {
    throw new Error(`Multica answered ${response.status}: ${text}`);
  }
  return text ? JSON.parse(text) : null;
}

async function loadPulse(body) {
  const stored = await callback(body, "GET", `/storage/workspace/${PULSE_KEY}`);
  if (!stored?.value) return { count: 0, delivery_id: "" };
  try {
    return JSON.parse(stored.value);
  } catch {
    return { count: 0, delivery_id: "" };
  }
}

const tlsCert = process.env.MULTICA_HOOK_TLS_CERT;
const tlsKey = process.env.MULTICA_HOOK_TLS_KEY;
const createServer = tlsCert && tlsKey
  ? (handler) => createHTTPSServer({ cert: readFileSync(tlsCert), key: readFileSync(tlsKey) }, handler)
  : createHTTPServer;

const server = createServer((request, response) => {
  if (request.method !== "POST" || request.url !== "/hooks/pulse") {
    response.writeHead(404).end();
    return;
  }

  const chunks = [];
  request.on("data", (chunk) => chunks.push(chunk));
  request.on("end", async () => {
    const rawBody = Buffer.concat(chunks);
    const failure = verify(rawBody, request.headers);
    if (failure) {
      console.warn(`rejected: ${failure}`);
      response.writeHead(401, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ error: failure }));
      return;
    }

    const body = JSON.parse(rawBody.toString("utf8"));
    if (body.hook_key !== "pulse" || body.trigger !== "schedule") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ skipped: "not a scheduled pulse" }));
      return;
    }
    if (!body.delivery_id) {
      response.writeHead(400, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ error: "scheduled delivery is missing delivery_id" }));
      return;
    }

    try {
      const previous = await loadPulse(body);
      if (previous.delivery_id === body.delivery_id) {
        console.log(`duplicate delivery ${body.delivery_id} attempt ${body.attempt}`);
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ duplicate: true, delivery_id: body.delivery_id, count: previous.count }));
        return;
      }

      const pulse = {
        delivery_id: body.delivery_id,
        count: (previous.count ?? 0) + 1,
        last_planned_at: body.schedule?.planned_at ?? "",
        last_occurred_at: body.occurred_at ?? "",
        last_attempt: body.attempt ?? 1,
      };
      await callback(body, "PUT", `/storage/workspace/${PULSE_KEY}`, { value: JSON.stringify(pulse) });
      console.log(`pulse ${pulse.count} delivery ${pulse.delivery_id} attempt ${pulse.last_attempt}`);
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ accepted: true, ...pulse }));
    } catch (error) {
      console.error(error);
      response.writeHead(502, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ error: "pulse failed" }));
    }
  });
});

server.listen(PORT, () => console.log(`schedule pulse listening on ${tlsCert ? "https" : "http"}://127.0.0.1:${PORT}`));
