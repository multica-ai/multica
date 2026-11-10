#!/usr/bin/env node
/**
 * Jira Link — the hook handler, run by the plugin author next to their Jira.
 *
 * The Jira credentials live here and nowhere else. Multica never sees them: the
 * manifest has no secret config, and the Jira site comes from this process's
 * environment rather than from installation config, so an admin editing config
 * cannot point these credentials at another host.
 *
 * What the handler writes back to Multica is issue metadata, under the `jira.`
 * prefix: `jira.key`, `jira.url` and `jira.status`. Metadata is where agents and
 * the UI already read pipeline state, so a linked issue is visible to both
 * without a second store.
 *
 * Run:
 *   MULTICA_SIGNING_SECRET=whsec_... \
 *   JIRA_SITE=https://acme.atlassian.net JIRA_EMAIL=bot@acme.com JIRA_API_TOKEN=... \
 *   node handler.mjs
 */

import { createServer as createHTTPServer } from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import { createHmac, timingSafeEqual } from "node:crypto";
import { readFileSync } from "node:fs";

const PORT = Number(process.env.PORT ?? 8788);
const SIGNING_SECRET = process.env.MULTICA_SIGNING_SECRET ?? "";
const JIRA_SITE = (process.env.JIRA_SITE ?? "").replace(/\/+$/, "");
const JIRA_EMAIL = process.env.JIRA_EMAIL ?? "";
const JIRA_API_TOKEN = process.env.JIRA_API_TOKEN ?? "";
const JIRA_AUTH = Buffer.from(`${JIRA_EMAIL}:${JIRA_API_TOKEN}`).toString("base64");
const TOLERANCE_SECONDS = 5 * 60;
const JIRA_KEY = /^[A-Z][A-Z0-9_]+-[1-9][0-9]*$/;

for (const [name, value] of Object.entries({ MULTICA_SIGNING_SECRET: SIGNING_SECRET, JIRA_SITE, JIRA_EMAIL, JIRA_API_TOKEN })) {
  if (!value) {
    console.error(`${name} is required.`);
    process.exit(1);
  }
}

// --- request verification (same contract as examples/plugins/triage-notify) ---

const seenSignatures = new Map();

function verify(rawBody, headers) {
  const timestamp = headers["x-multica-timestamp"];
  const presented = String(headers["x-multica-signature"] ?? "").replace(/^v1=/, "");
  if (!timestamp || !presented) return "missing signature headers";

  const drift = Math.abs(Math.floor(Date.now() / 1000) - Number(timestamp));
  if (!Number.isFinite(drift) || drift > TOLERANCE_SECONDS) return "timestamp outside the accepted window";

  const secret = Buffer.from(SIGNING_SECRET.replace(/^whsec_/, ""), "hex");
  const expected = Buffer.from(createHmac("sha256", secret).update(timestamp).update(".").update(rawBody).digest("hex"));
  const actual = Buffer.from(presented);
  if (expected.length !== actual.length || !timingSafeEqual(expected, actual)) return "signature does not match";

  const now = Date.now();
  for (const [seen, at] of seenSignatures) {
    if (now - at > TOLERANCE_SECONDS * 1000) seenSignatures.delete(seen);
  }
  if (seenSignatures.has(presented)) return "this request was already delivered";
  seenSignatures.set(presented, now);
  return null;
}

// --- the two APIs this handler talks to ---

async function multica(body, method, path, payload) {
  const response = await fetch(`${body.callback_url}${path}`, {
    method,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${body.callback_token}` },
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(`Multica answered ${response.status} for ${method} ${path}`);
  return response.json();
}

async function jira(method, path, payload) {
  const response = await fetch(`${JIRA_SITE}/rest/api/3${path}`, {
    method,
    headers: { Accept: "application/json", "Content-Type": "application/json", Authorization: `Basic ${JIRA_AUTH}` },
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`Jira answered ${response.status} for ${method} ${path}`);
  return response.status === 204 ? {} : response.json();
}

/** Writes one metadata key. Writing the stored value again is a no-op upstream. */
function setMetadata(body, issueId, key, value) {
  return multica(body, "PUT", `/issues/${encodeURIComponent(issueId)}/metadata/${encodeURIComponent(key)}`, { value });
}

async function recordLink(body, issueId, jiraIssue) {
  await setMetadata(body, issueId, "jira.key", jiraIssue.key);
  await setMetadata(body, issueId, "jira.url", `${JIRA_SITE}/browse/${jiraIssue.key}`);
  if (jiraIssue.fields?.status?.name) {
    await setMetadata(body, issueId, "jira.status", jiraIssue.fields.status.name);
  }
}

// Jira workflows differ per project, but every Jira status belongs to one of
// three fixed categories. Mapping on the category, not on status names, is what
// lets one plugin work against any workflow.
function jiraCategoryFor(issue) {
  switch (issue.status_category || issue.status) {
    case "backlog":
    case "todo":
    case "unstarted":
      return "new";
    case "in_progress":
    case "in_review":
    case "started":
      return "indeterminate";
    case "done":
    case "closed":
      return "done";
    default:
      return null; // cancelled, blocked, custom: leave Jira alone
  }
}

// --- hooks ---
//
// Each hook resolves to the JSON body of a 200. Expected outcomes, including
// "that key is not in Jira", are 200s with an `error` field: the host treats
// any other status as the hook failing and does not pass the body on, so the
// panel could not tell the person what was wrong. Only real failures (Jira or
// Multica unreachable, an unexpected answer) throw and become a 502.

const hooks = {
  async link_issue(body, issueId) {
    const key = String(body.input?.key ?? "").trim().toUpperCase();
    if (!JIRA_KEY.test(key)) return { error: "not a Jira issue key" };
    const jiraIssue = await jira("GET", `/issue/${encodeURIComponent(key)}?fields=status`);
    if (!jiraIssue) return { error: `${key} was not found in Jira` };
    await recordLink(body, issueId, jiraIssue);
    return { linked: jiraIssue.key };
  },

  async create_issue(body, issueId) {
    const issue = await multica(body, "GET", `/issues/${encodeURIComponent(issueId)}`);
    if (issue.metadata?.["jira.key"]) return { linked: issue.metadata["jira.key"] };
    const projectKey = String(body.config?.project_key ?? "").trim();
    if (!/^[A-Z][A-Z0-9_]+$/.test(projectKey)) return { error: "project_key must be a Jira project key such as OPS" };

    // Jira can accept the create after Multica has given up on this call (a
    // timeout revokes the callback token before the link is recorded). The
    // label makes a retry find that issue instead of creating a second one.
    const label = `multica-${issueId}`;
    const search = await jira("POST", "/search/jql", {
      jql: `project = "${projectKey}" AND labels = "${label}" ORDER BY created ASC`,
      fields: ["status"],
      maxResults: 1,
    });
    const existing = search?.issues ?? [];
    if (existing.length > 0) {
      await recordLink(body, issueId, existing[0]);
      return { linked: existing[0].key };
    }

    const created = await jira("POST", "/issue", {
      fields: {
        project: { key: projectKey },
        issuetype: { name: "Task" },
        labels: [label],
        summary: issue.title,
        description: {
          type: "doc",
          version: 1,
          content: [{ type: "paragraph", content: [{ type: "text", text: `${issue.identifier}: ${issue.description || issue.title}` }] }],
        },
      },
    });
    await recordLink(body, issueId, created);
    return { linked: created.key };
  },

  async sync_status(body, issueId) {
    const issue = await multica(body, "GET", `/issues/${encodeURIComponent(issueId)}`);
    const key = issue.metadata?.["jira.key"];
    const category = jiraCategoryFor(issue);
    if (!key || !category) return { skipped: key ? "unmapped status" : "not linked" };

    const current = await jira("GET", `/issue/${encodeURIComponent(key)}?fields=status`);
    if (!current) return { skipped: `${key} no longer exists` };
    // Already there: nothing to move. Also what stops a Jira-side sync from
    // bouncing a change back and forth.
    if (current.fields?.status?.statusCategory?.key === category) {
      return { unchanged: key };
    }
    const transitions = (await jira("GET", `/issue/${encodeURIComponent(key)}/transitions`))?.transitions ?? [];
    const transition = transitions.find((candidate) => candidate.to?.statusCategory?.key === category);
    if (!transition) return { skipped: `no transition into ${category}` };
    await jira("POST", `/issue/${encodeURIComponent(key)}/transitions`, { transition: { id: transition.id } });
    await setMetadata(body, issueId, "jira.status", transition.to.name);
    return { transitioned: key, to: transition.to.name };
  },
};

// --- server ---

const tlsCert = process.env.MULTICA_HOOK_TLS_CERT;
const tlsKey = process.env.MULTICA_HOOK_TLS_KEY;
const createServer = tlsCert && tlsKey
  ? (handler) => createHTTPSServer({ cert: readFileSync(tlsCert), key: readFileSync(tlsKey) }, handler)
  : createHTTPServer;

function reply(response, status, result) {
  response.writeHead(status, { "Content-Type": "application/json" });
  response.end(JSON.stringify(result));
}

const server = createServer((request, response) => {
  if (request.method !== "POST" || !request.url?.startsWith("/hooks/")) {
    response.writeHead(404).end();
    return;
  }
  const chunks = [];
  request.on("data", (chunk) => chunks.push(chunk));
  request.on("end", async () => {
    const rawBody = Buffer.concat(chunks);
    const failure = verify(rawBody, request.headers);
    if (failure) return reply(response, 401, { error: failure });

    const body = JSON.parse(rawBody.toString("utf8"));
    const hook = hooks[body.hook_key];
    // The host resolved and permission-checked this issue; the callback token
    // reaches no other.
    if (!hook || !body.issue_id) return reply(response, 200, { skipped: "nothing to do" });
    try {
      const result = await hook(body, body.issue_id);
      console.log(`${body.hook_key} via ${body.trigger}: ${JSON.stringify(result)}`);
      reply(response, 200, result);
    } catch (error) {
      console.error(error);
      reply(response, 502, { error: `${body.hook_key} failed` });
    }
  });
});

server.listen(PORT, () => console.log(`jira-link handler listening on ${tlsCert ? "https" : "http"}://127.0.0.1:${PORT}`));
