// Deploy Sentinel's own server. Multica never runs this — the plugin author does.
//
// Everything a real hook backend has to get right is here and nowhere else:
// verifying the signature, refusing a replay, applying the team's own rules, and
// using the per-invocation callback token to write back to the issue.
//
//   node server/handler.mjs
//
// Env: MULTICA_SIGNING_SECRET (the whsec_… shown once when the token was issued)
//      PORT (default 8788)

import { createServer } from "node:https";
import { readFileSync } from "node:fs";
import { createHmac, timingSafeEqual } from "node:crypto";

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

const PORT = Number(process.env.PORT ?? 8788);
const SIGNING_SECRET = process.env.MULTICA_SIGNING_SECRET ?? "";
const REPLAY_WINDOW_SECONDS = 300;

// Stand-in for the deploy system this plugin would really talk to. Deploy ids
// are stable so the README's walkthrough is reproducible.
const DEPLOYS = [
  { id: "d-4821", service: "checkout-api", minutes_ago: 4, commit: "9f2c1ab", author: "rivera", blast_radius: "all checkout traffic", touches: ["retry path", "timeout config"] },
  { id: "d-4820", service: "checkout-api", minutes_ago: 51, commit: "1de77c0", author: "okafor", blast_radius: "checkout traffic in EU", touches: ["currency formatting"] },
  { id: "d-4816", service: "search-api", minutes_ago: 92, commit: "aa30f41", author: "rivera", blast_radius: "search suggestions", touches: ["index warmup"] },
  { id: "d-4802", service: "checkout-api", minutes_ago: 380, commit: "77b9e2d", author: "chen", blast_radius: "all checkout traffic", touches: ["payment provider client"] },
];

const filedRollbacks = new Map();

function verifySignature(rawBody, signature, timestamp) {
  if (!SIGNING_SECRET) return { ok: false, reason: "server has no signing secret configured" };
  if (!signature || !timestamp) return { ok: false, reason: "missing signature headers" };

  // Reject a replay before spending time on the comparison. Multica signs
  // timestamp + body precisely so an old, validly-signed request cannot be
  // resent later.
  const age = Math.abs(Math.floor(Date.now() / 1000) - Number(timestamp));
  if (!Number.isFinite(age) || age > REPLAY_WINDOW_SECONDS) {
    return { ok: false, reason: "timestamp outside the replay window" };
  }

  const expected = createHmac("sha256", SIGNING_SECRET).update(`${timestamp}.${rawBody}`).digest("hex");
  const provided = Buffer.from(signature, "utf8");
  const computed = Buffer.from(expected, "utf8");
  // Length check first: timingSafeEqual throws on a mismatch rather than
  // returning false, and the length is not the secret.
  if (provided.length !== computed.length || !timingSafeEqual(provided, computed)) {
    return { ok: false, reason: "signature mismatch" };
  }
  return { ok: true };
}

// The callback token is scoped to THIS invocation and is revoked the moment the
// hook returns, so anything the handler wants to write has to be written before
// it replies. That constraint is the reason this is awaited, not fired off.
async function commentOnIssue(callbackUrl, callbackToken, issueId, content) {
  if (!callbackToken || !callbackUrl || !issueId) return;
  try {
    const response = await fetch(`${callbackUrl}/issues/${encodeURIComponent(issueId)}/comments`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${callbackToken}` },
      body: JSON.stringify({ content }),
    });
    if (!response.ok) {
      console.warn(`callback comment failed: ${response.status} ${await response.text()}`);
    }
  } catch (error) {
    // A failed write-back must not fail the hook: the agent still gets its
    // answer, and a missing comment is recoverable where a failed tool is not.
    console.warn("callback comment error:", error.message);
  }
}

function correlateDeploys(input, config) {
  const windowMinutes = Number(input.window_minutes ?? 120);
  const service = `${config.service_prefix ?? ""}${input.service ?? ""}`;

  const matches = DEPLOYS
    .filter((deploy) => `${config.service_prefix ?? ""}${deploy.service}` === service)
    .filter((deploy) => deploy.minutes_ago <= windowMinutes)
    .sort((a, b) => a.minutes_ago - b.minutes_ago);

  return {
    service,
    window_minutes: windowMinutes,
    deploy_count: matches.length,
    deploys: matches,
    // Said explicitly rather than left for the agent to infer from an empty
    // array. "Nothing deployed" is a real finding and the skill tells the agent
    // to report it rather than keep digging.
    summary: matches.length === 0
      ? `No deploys to ${service} in the last ${windowMinutes} minutes. This is unlikely to be a deploy.`
      : `${matches.length} deploy(s) to ${service} in the last ${windowMinutes} minutes; most recent is ${matches[0].id} (${matches[0].minutes_ago}m ago, ${matches[0].commit} by ${matches[0].author}).`,
  };
}

function requestRollback(input, config) {
  const deploy = DEPLOYS.find((entry) => entry.id === input.deploy_id);
  if (!deploy) {
    return { status: "rejected", reason: `Unknown deploy ${input.deploy_id}.` };
  }

  const reason = String(input.reason ?? "").trim();
  if (reason.length < 20) {
    // The approver reads this at 3am; a one-word reason wastes their time.
    return {
      status: "rejected",
      reason: "A rollback request needs a written reason (at least 20 characters) describing the evidence, not the conclusion.",
    };
  }

  const windowMinutes = Number(config.rollback_window_minutes ?? 60);
  if (deploy.minutes_ago > windowMinutes) {
    return {
      status: "rejected",
      reason: `Deploy ${deploy.id} landed ${deploy.minutes_ago} minutes ago, past the ${windowMinutes}-minute rollback window. Past that point a fix forward is usually safer, and it needs a human deciding.`,
    };
  }

  const changeId = `chg-${String(filedRollbacks.size + 1).padStart(4, "0")}`;
  filedRollbacks.set(changeId, { deploy: deploy.id, reason });
  return {
    status: "filed",
    change_id: changeId,
    deploy_id: deploy.id,
    environment: config.environment ?? "production",
    // Stated so the agent does not report the incident as handled.
    next_step: "A human must approve this change request. Nothing has been rolled back yet.",
  };
}

const server = createServer(tlsOptions(), async (req, res) => {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const rawBody = Buffer.concat(chunks).toString("utf8");

  const reply = (status, payload) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify(payload));
  };

  const verified = verifySignature(
    rawBody,
    req.headers["x-multica-signature"],
    req.headers["x-multica-timestamp"],
  );
  if (!verified.ok) {
    console.warn(`refused ${req.url}: ${verified.reason}`);
    return reply(401, { error: verified.reason });
  }

  let payload;
  try {
    payload = JSON.parse(rawBody || "{}");
  } catch {
    return reply(400, { error: "body is not valid JSON" });
  }

  const {
    hook_key: hookKey,
    trigger,
    input = {},
    config = {},
    callback_url: callbackUrl,
    callback_token: callbackToken,
    issue_id: issueId,
  } = payload;
  console.log(`${hookKey} via ${trigger}`);

  switch (hookKey) {
    case "correlate_deploys": {
      const result = correlateDeploys(input, config);
      // A ui/manual click is a person asking; leave the answer where they and
      // their colleagues will see it. An agent call already returns to the
      // agent, which will write its own account of what it found.
      if (trigger === "ui" || trigger === "manual") {
        await commentOnIssue(callbackUrl, callbackToken, issueId, `**Deploy Sentinel** — ${result.summary}`);
      }
      return reply(200, result);
    }

    case "request_rollback": {
      const result = requestRollback(input, config);
      if (result.status === "filed") {
        await commentOnIssue(
          callbackUrl,
          callbackToken,
          issueId,
          `**Deploy Sentinel** filed rollback ${result.change_id} for deploy ${result.deploy_id}.\n\n> ${input.reason}\n\nAwaiting human approval — nothing has been rolled back.`,
        );
      }
      return reply(200, result);
    }

    case "page_oncall": {
      // An event hook's response is discarded, so there is nothing to return
      // that anybody reads. Acknowledge fast and do the work; blocking here
      // would hold a Multica worker for no benefit.
      const issue = payload.event?.issue ?? {};
      console.log(`paging on-call for ${payload.event?.type}: ${issue.title ?? issueId}`);
      return reply(202, { acknowledged: true });
    }

    default:
      return reply(404, { error: `unknown hook ${hookKey}` });
  }
});

server.listen(PORT, () => {
  console.log(`Deploy Sentinel listening on https://127.0.0.1:${PORT}`);
  if (!SIGNING_SECRET) {
    console.warn("MULTICA_SIGNING_SECRET is not set — every request will be refused.");
  }
});
