#!/usr/bin/env node
/**
 * A reference hook handler — the plugin author's side of the contract.
 *
 * This is the half that does NOT run in Multica. It is an ordinary HTTP server
 * the author operates; Multica only ever sends it a signed POST. Everything
 * security-relevant here is on this side of the wire, which is the point: the
 * host cannot make a handler safe, it can only give it what it needs to be.
 *
 * Run:
 *   MULTICA_SIGNING_SECRET=whsec_... node handler.mjs
 *
 * The signing secret is shown once, next to the install token, when an admin
 * rotates the plugin's token in workspace settings.
 */

import { createServer as createHTTPServer } from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import { createHmac, timingSafeEqual } from "node:crypto";
import { readFileSync } from "node:fs";

const PORT = Number(process.env.PORT ?? 8787);
const SIGNING_SECRET = process.env.MULTICA_SIGNING_SECRET ?? "";
const TOLERANCE_SECONDS = 5 * 60;

if (!SIGNING_SECRET) {
  console.error("MULTICA_SIGNING_SECRET is required. Rotate the plugin token in Multica to obtain it.");
  process.exit(1);
}

/**
 * Replay protection, the half the host cannot do for you.
 *
 * The timestamp in the signature bounds a captured request to a few minutes.
 * Remembering the signatures seen inside that window closes the rest: the same
 * bytes cannot be delivered twice, even immediately. Without this, an attacker
 * who observes one valid request can repeat it until the window expires.
 */
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

  // Constant time. A byte-at-a-time comparison leaks how much of a guessed
  // signature was correct, which is enough to recover the rest.
  const expectedBytes = Buffer.from(expected, "utf8");
  const presentedBytes = Buffer.from(presented, "utf8");
  if (expectedBytes.length !== presentedBytes.length) return "signature does not match";
  if (!timingSafeEqual(expectedBytes, presentedBytes)) return "signature does not match";

  if (!rememberSignature(presented, Date.now())) return "this request was already delivered";
  return null;
}

/**
 * Calls Multica back using the one-shot token that arrived with the request.
 *
 * The token is scoped to this invocation and expires in minutes, so it is worth
 * spending on the work at hand rather than storing. It is good for as many calls
 * as the job needs — reading the issue and then commenting on it is two, which
 * is the floor for a handler that does anything with what it read. Which identity the writes
 * land under was decided when the hook was dispatched: a ui/manual call writes
 * as the person who triggered it, an event call writes as the plugin.
 */
async function callback(body, method, path, payload) {
  const response = await fetch(`${body.callback_url}${path}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${body.callback_token}`,
    },
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(`Multica answered ${response.status}: ${await response.text()}`);
  }
  return response.json();
}

/** Stand-in for whatever the author's real triage service does. */
function triage(issue) {
  const text = `${issue.title ?? ""} ${issue.description ?? ""}`.toLowerCase();
  if (text.includes("crash") || text.includes("data loss")) {
    return { priority: "urgent", owner: "on-call" };
  }
  if (text.includes("slow") || text.includes("timeout")) {
    return { priority: "high", owner: "performance" };
  }
  return { priority: "medium", owner: "triage" };
}

// TLS when a cert is supplied. A hook transport URL must be HTTPS — the
// manifest validator requires it — so a handler that only speaks HTTP cannot be
// pointed at even in development.
const tlsCert = process.env.MULTICA_HOOK_TLS_CERT;
const tlsKey = process.env.MULTICA_HOOK_TLS_KEY;
const createServer = tlsCert && tlsKey
  ? (handler) => createHTTPSServer({ cert: readFileSync(tlsCert), key: readFileSync(tlsKey) }, handler)
  : createHTTPServer;

const server = createServer((request, response) => {
  if (request.method !== "POST" || !request.url?.startsWith("/hooks/triage")) {
    response.writeHead(404).end();
    return;
  }

  const chunks = [];
  request.on("data", (chunk) => chunks.push(chunk));
  request.on("end", async () => {
    // Verify against the RAW bytes, before any parsing. Re-serializing JSON
    // and signing that instead is the classic way to make a valid signature
    // fail and an invalid one pass — key order and spacing are not preserved.
    const rawBody = Buffer.concat(chunks);
    const failure = verify(rawBody, request.headers);
    if (failure) {
      console.warn(`rejected: ${failure}`);
      response.writeHead(401, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ error: failure }));
      return;
    }

    const body = JSON.parse(rawBody.toString("utf8"));
    console.log(`hook ${body.hook_key} via ${body.trigger}${body.event_type ? ` (${body.event_type})` : ""} as ${body.actor?.type}`);

    try {
      // The host tells us which issue this is about, having already resolved and
      // permission-checked it. Preferred over anything in `input`: for an event
      // trigger there was no client to supply it, and for ui/manual a
      // client-supplied id is not something a handler should trust.
      const issueId = body.issue_id ?? body.input?.issue_id ?? body.input?.issue?.id;
      if (!issueId) {
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ skipped: "no issue in scope" }));
        return;
      }

      const issue = await callback(body, "GET", `/issues/${encodeURIComponent(issueId)}`);
      const verdict = triage(issue);
      await callback(body, "POST", `/issues/${encodeURIComponent(issueId)}/comments`, {
        content: `Triage: **${verdict.priority}**, suggested owner **${verdict.owner}**.`,
      });

      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify(verdict));
    } catch (error) {
      console.error(error);
      // Answer with a status, not with the internal error text: whatever we
      // return is the host's problem to classify, and echoing internals here
      // is how a handler leaks its own configuration into someone's log.
      response.writeHead(502, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ error: "triage failed" }));
    }
  });
});

server.listen(PORT, () => console.log(`triage handler listening on ${tlsCert ? "https" : "http"}://127.0.0.1:${PORT}`));
