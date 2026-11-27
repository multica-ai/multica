#!/usr/bin/env node
// A local stand-in for Composio, so Connected Apps runs in a `make up`
// environment without a Composio project:
//
//   make up C=api,web ARGS="--ephemeral"
//   node scripts/fake-composio.mjs up       # start it and point this environment's API at it
//   node scripts/fake-composio.mjs status
//   node scripts/fake-composio.mjs down     # stop it and take the API's Composio settings back out
//
// It serves the part of the Composio v3.1 REST API that server/pkg/composio
// calls (toolkits, auth configs, connect links, connected accounts, tools, tool
// execution and tool-router sessions), plus a stand-in for the hosted connect
// page: Allow finishes the connection, Cancel fails it. OAuth toolkits come
// with a Composio-managed auth config; API-key toolkits get one when Multica
// creates it, and their connect page asks for the key. Tool calls return
// canned data.
//
// `up` appends a marked block to the environment's env file with
// COMPOSIO_API_KEY, COMPOSIO_BASE_URL, COMPOSIO_STATE_SECRET and
// FF_COMPOSIO_MCP_APPS=true, restarts a running API so it reads them, and runs
// the fake detached on a free loopback port, logging to the environment's log
// directory. The fake exits by itself once the environment is destroyed (its
// manifest disappears). `down` stops it and removes the block; values the file
// had before the block apply again. State lives in memory, so accounts
// connected before a restart of the fake are unknown to it afterwards, while
// Multica keeps its own rows for them.
//
// `serve [--port N]` runs it in the foreground without touching any
// environment, for setups other than `make up`.
//
// Every response has the shape Composio's v3.1 OpenAPI spec gives it, down to
// the fields it leaves out: `node --test scripts/fake-composio.test.mjs` calls
// each endpoint and checks the answer against the snapshot of that spec in
// server/pkg/composio/composiotest/openapi-v3.1.json. Refresh the snapshot with
// `node scripts/composio-spec.mjs update`, then rerun the test; a new endpoint
// goes into OPERATIONS in scripts/composio-spec.mjs first.

import { spawn, spawnSync } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { existsSync, mkdirSync, openSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import http from "node:http";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPT = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(dirname(SCRIPT), "..");
const API_PREFIX = "/api/v3.1";
const HOST = "127.0.0.1";
const API_KEY = "fake-composio";
const BLOCK_BEGIN = "# >>> fake-composio: `node scripts/fake-composio.mjs down` removes this block";
const BLOCK_END = "# <<< fake-composio";

const USAGE = `Usage: node scripts/fake-composio.mjs <up | down | status | serve> [--port N]

  up      start the fake for this checkout's \`make up\` environment and point its API at it
  down    stop it and remove its settings from the environment's env file
  status  show whether it runs, on which port, and whether the env file points at it
  serve   run it in the foreground; --port picks the port (default: any free port)`;

function fail(message) {
  console.error(`[fake-composio] ${message}`);
  process.exit(1);
}

const log = (message) => console.log(`[fake-composio] ${message}`);
const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

// ------------------------------------------------------------------ catalog ---

const COLLAB = "Collaboration & Communication";
const PRODUCTIVITY = "Productivity & Project Management";
const DOCS = "Document & File Management";
const DEV = "Developer Tools & DevOps";
const DATA = "Data & Analytics";
const AI = "AI & Machine Learning";
const MARKETING = "Marketing & Social Media";
const SUPPORT = "Sales & Customer Support";

// [slug, name, category, auth]: "managed" toolkits sign in through Composio's
// own OAuth app and come with an auth config; "api_key" and "bearer_token"
// toolkits have none until Multica creates one, and their connect page asks
// for the key. "none" toolkits need no account, so Multica does not list them.
const TOOLKITS = [
  ["gmail", "Gmail", COLLAB, "managed"],
  ["googlecalendar", "Google Calendar", "Scheduling & Booking", "managed"],
  ["outlook", "Outlook", COLLAB, "managed"],
  ["slack", "Slack", COLLAB, "managed"],
  ["notion", "Notion", PRODUCTIVITY, "managed"],
  ["googledrive", "Google Drive", DOCS, "managed"],
  ["googledocs", "Google Docs", DOCS, "managed"],
  ["googlesheets", "Google Sheets", PRODUCTIVITY, "managed"],
  ["confluence", "Confluence", COLLAB, "managed"],
  ["linear", "Linear", PRODUCTIVITY, "managed"],
  ["jira", "Jira", PRODUCTIVITY, "managed"],
  ["asana", "Asana", PRODUCTIVITY, "managed"],
  ["figma", "Figma", "Design & Creative Tools", "managed"],
  ["sentry", "Sentry", DEV, "managed"],
  ["posthog", "PostHog", DATA, "api_key"],
  ["hubspot", "HubSpot", "CRM", "managed"],
  ["zendesk", "Zendesk", SUPPORT, "managed"],
  ["intercom", "Intercom", SUPPORT, "managed"],
  ["stripe", "Stripe", "Finance & Accounting", "managed"],
  ["airtable", "Airtable", PRODUCTIVITY, "managed"],
  ["github", "GitHub", DEV, "managed"],
  ["discord", "Discord", COLLAB, "managed"],
  ["trello", "Trello", PRODUCTIVITY, "managed"],
  ["dropbox", "Dropbox", DOCS, "managed"],
  ["mailchimp", "Mailchimp", MARKETING, "managed"],
  ["tavily", "Tavily", DATA, "api_key"],
  ["exa", "Exa", DATA, "bearer_token"],
  ["firecrawl", "Firecrawl", DATA, "api_key"],
  ["openai", "OpenAI", AI, "api_key"],
  ["perplexityai", "Perplexity AI", AI, "api_key"],
  ["elevenlabs", "ElevenLabs", AI, "api_key"],
  ["sendgrid", "SendGrid", MARKETING, "api_key"],
  ["datadog", "Datadog", DEV, "api_key"],
  ["vercel", "Vercel", DEV, "bearer_token"],
  ["shopify", "Shopify", "E-commerce", "api_key"],
  ["hackernews", "Hacker News", "Entertainment & Media", "none"],
].map(([slug, name, category, auth]) => ({
  slug,
  name,
  category: { id: category.toLowerCase().replace(/[^a-z0-9]+/g, "-"), name: category },
  managed: auth === "managed",
  // Upper-case auth modes, as auth_config_details and auth configs name them.
  modes: auth === "managed" ? ["OAUTH2"] : auth === "none" ? ["NO_AUTH"] : [auth.toUpperCase()],
}));

const VERSION = "20250905_00";
const EPOCH = "2025-01-01T00:00:00.000Z";
const logoOf = (slug) => `https://logos.composio.dev/api/${slug}`;

function toolkitMeta(toolkit) {
  return {
    created_at: EPOCH,
    updated_at: EPOCH,
    description: `${toolkit.name} through the local Composio stand-in.`,
    logo: logoOf(toolkit.slug),
    triggers_count: 0,
    tools_count: toolsOf(toolkit.slug).length,
    version: VERSION,
  };
}

// The list reports auth schemes as lower-case auth_schemes and its categories
// as {id, name}.
function listedToolkit(toolkit) {
  const noAuth = toolkit.modes[0] === "NO_AUTH";
  return {
    slug: toolkit.slug,
    name: toolkit.name,
    type: "native",
    app: toolkit.name,
    toolkit_type: "api",
    auth_schemes: noAuth ? [] : toolkit.modes.map((mode) => mode.toLowerCase()),
    composio_managed_auth_schemes: toolkit.managed ? ["oauth2"] : [],
    is_local_toolkit: false,
    no_auth: noAuth,
    deprecated: { toolkitId: `tk_${toolkit.slug}` },
    meta: { ...toolkitMeta(toolkit), categories: [toolkit.category] },
  };
}

function credentialField(mode) {
  const [name, displayName] = mode === "BEARER_TOKEN" ? ["token", "Token"] : mode === "BASIC" ? ["password", "Password"] : ["generic_api_key", "API Key"];
  return { name, displayName, type: "string", description: `Your ${displayName}.`, required: true, default: null, is_secret: true, user_visible: true };
}

// A single toolkit has no auth_schemes or no_auth: each scheme is an
// auth_config_details entry. Its categories are {name, slug}.
function toolkitDetails(toolkit) {
  return {
    slug: toolkit.slug,
    name: toolkit.name,
    type: "native",
    toolkit_type: "api",
    enabled: true,
    composio_managed_auth_schemes: toolkit.managed ? ["oauth2"] : [],
    composio_managed_auth: toolkit.managed
      ? [{ managed_app_id: `ma_${toolkit.slug}`, is_default: true, mode: "OAUTH2", scopes: { available: [] } }]
      : [],
    is_local_toolkit: false,
    auth_config_details: toolkit.modes.map((mode) => ({
      mode,
      name: `${toolkit.slug}_${mode.toLowerCase()}`,
      required_scopes: [],
      is_connection_revoke_supported: mode === "OAUTH2",
      fields: {
        auth_config_creation: { required: [], optional: [] },
        connected_account_initiation: {
          required: mode === "OAUTH2" || mode === "NO_AUTH" ? [] : [credentialField(mode)],
          optional: [],
        },
      },
    })),
    docs_url: null,
    meta: {
      ...toolkitMeta(toolkit),
      categories: [{ name: toolkit.category.name, slug: toolkit.category.id }],
      available_versions: [VERSION],
    },
    deprecated: { toolkitId: `tk_${toolkit.slug}`, rawProxyInfoByAuthSchemes: [] },
  };
}

const page = (items) => ({ items, next_cursor: null, total_pages: 1, current_page: 1, total_items: items.length });

// Slugs follow Composio's VERB_OBJECT naming, which is what the gateway
// classifies into read / write / send / delete.
const TOOLS = {
  gmail: [
    ["GMAIL_FETCH_EMAILS", "Fetch emails", "Fetch a list of email messages from the mailbox."],
    ["GMAIL_SEND_EMAIL", "Send email", "Send an email to one or more recipients."],
    ["GMAIL_CREATE_EMAIL_DRAFT", "Create email draft", "Create a draft email."],
    ["GMAIL_DELETE_MESSAGE", "Delete message", "Permanently delete an email message."],
  ],
  googlecalendar: [
    ["GOOGLECALENDAR_FIND_EVENT", "Find events", "Find events in a calendar."],
    ["GOOGLECALENDAR_FIND_FREE_SLOTS", "Find free slots", "Find free time across calendars."],
    ["GOOGLECALENDAR_CREATE_EVENT", "Create event", "Create an event and invite its attendees."],
    ["GOOGLECALENDAR_DELETE_EVENT", "Delete event", "Delete an event."],
  ],
  slack: [
    ["SLACK_LIST_ALL_CHANNELS", "List channels", "List the channels of the workspace."],
    ["SLACK_SEND_MESSAGE", "Send message", "Post a message to a channel."],
  ],
  notion: [
    ["NOTION_SEARCH_NOTION_PAGE", "Search pages", "Search Notion pages."],
    ["NOTION_CREATE_NOTION_PAGE", "Create page", "Create a Notion page."],
  ],
  linear: [
    ["LINEAR_LIST_LINEAR_ISSUES", "List issues", "List Linear issues."],
    ["LINEAR_CREATE_LINEAR_ISSUE", "Create issue", "Create a Linear issue."],
  ],
  sentry: [
    ["SENTRY_LIST_ISSUES", "List issues", "List issues of a Sentry project."],
    ["SENTRY_GET_ISSUE", "Get issue", "Retrieve one Sentry issue."],
    ["SENTRY_UPDATE_ISSUE", "Update issue", "Update a Sentry issue (resolve, assign)."],
  ],
  posthog: [
    ["POSTHOG_LIST_INSIGHTS", "List insights", "List the saved insights of a project."],
    ["POSTHOG_GET_INSIGHT", "Get insight", "Retrieve one insight with its results."],
    ["POSTHOG_CREATE_ANNOTATION", "Create annotation", "Annotate a date on the project's charts."],
  ],
  hubspot: [
    ["HUBSPOT_SEARCH_CONTACTS", "Search contacts", "Search CRM contacts."],
    ["HUBSPOT_CREATE_CONTACT", "Create contact", "Create a CRM contact."],
  ],
};

function toolsOf(slug) {
  if (TOOLS[slug]) return TOOLS[slug];
  const prefix = slug.toUpperCase();
  return [
    [`${prefix}_SEARCH_RECORDS`, "Search records", "Search records."],
    [`${prefix}_CREATE_RECORD`, "Create record", "Create a record."],
    [`${prefix}_DELETE_RECORD`, "Delete record", "Delete a record."],
  ];
}

const toolView = (slug, [toolSlug, name, description]) => ({
  slug: toolSlug,
  name,
  description,
  toolkit: { slug, name: TOOLKITS.find((t) => t.slug === slug)?.name ?? slug, logo: logoOf(slug) },
  input_parameters: {
    type: "object",
    properties: { query: { type: "string", description: "Free text the fake echoes back." } },
  },
  output_parameters: { type: "object", properties: {} },
  no_auth: false,
  version: VERSION,
  available_versions: [VERSION],
  scopes: [],
  scope_requirements: null,
  tags: [],
  is_deprecated: false,
  deprecated: { displayName: name, version: VERSION, available_versions: [VERSION], is_deprecated: false, toolkit: { logo: logoOf(slug) } },
});

function findTool(toolSlug) {
  const upper = toolSlug.toUpperCase();
  for (const { slug } of TOOLKITS) {
    const tool = toolsOf(slug).find(([candidate]) => candidate === upper);
    if (tool) return toolView(slug, tool);
  }
  return null;
}

function cannedResult(toolSlug, args) {
  if (toolSlug === "GMAIL_FETCH_EMAILS") {
    return {
      messages: [
        { id: "m1", from: "ops@example.com", subject: "Weekly report", snippet: "Numbers attached." },
        { id: "m2", from: "billing@example.com", subject: "Invoice #1042", snippet: "Due next Friday." },
      ],
    };
  }
  if (/_(LIST|SEARCH|FIND|FETCH|GET)_/.test(`${toolSlug}_`)) {
    return { items: [{ id: "1", title: "Sample record from the local Composio stand-in" }], arguments: args };
  }
  return { ok: true, id: `fake_${Date.now().toString(36)}`, arguments: args };
}

// ------------------------------------------------------------------- server ---

// Credentials a connected account's state carries, per auth scheme: the spec
// requires them on an ACTIVE or INACTIVE account.
const STATE_CREDENTIALS = {
  OAUTH2: { access_token: "fake-access-token", token_type: "Bearer" },
  BEARER_TOKEN: { token: "fake-token" },
  BASIC: { username: "fake-user" },
  API_KEY: {},
};

function serve({ port, stateFile, watchFile }) {
  const newAuthConfig = (slug, scheme, managed) => ({ id: `ac_${slug}`, uuid: randomUUID(), scheme, managed, created_at: new Date().toISOString() });
  const authConfigs = new Map(TOOLKITS.filter((t) => t.managed).map((t) => [t.slug, newAuthConfig(t.slug, "OAUTH2", true)]));
  const links = new Map();
  const accounts = new Map();
  let seq = 1;
  let baseURL = "";

  const slugOfAuthConfig = (id) => [...authConfigs.entries()].find(([, config]) => config.id === id)?.[0];
  const toolkitName = (slug) => TOOLKITS.find((t) => t.slug === slug)?.name ?? slug;

  function send(res, code, body) {
    res.writeHead(code, { "content-type": "application/json" });
    res.end(JSON.stringify(body));
  }

  function sendError(res, code, slug, message) {
    send(res, code, { error: { message, code, slug, status: code } });
  }

  function authConfigView(slug, config) {
    return {
      id: config.id,
      uuid: config.uuid,
      type: config.managed ? "default" : "custom",
      toolkit: { slug, logo: logoOf(slug) },
      name: `${toolkitName(slug)} (fake)`,
      auth_scheme: config.scheme,
      is_composio_managed: config.managed,
      managed_app_id: config.managed ? `ma_${slug}` : null,
      status: "ENABLED",
      created_at: config.created_at,
      last_updated_at: config.created_at,
      no_of_connections: [...accounts.values()].filter((account) => account.auth_config_id === config.id).length,
      tool_access_config: {},
    };
  }

  function accountView(account) {
    const credentials = account.status === "ACTIVE" || account.status === "INACTIVE" ? STATE_CREDENTIALS[account.scheme] : {};
    return {
      id: account.id,
      user_id: account.user_id,
      toolkit: { slug: account.slug },
      auth_config: { id: account.auth_config_id, auth_scheme: account.scheme, is_composio_managed: account.managed, is_disabled: false },
      status: account.status,
      status_reason: null,
      is_disabled: false,
      word_id: null,
      alias: account.alias ?? null,
      state: { authScheme: account.scheme, val: { status: account.status, ...credentials } },
      data: {},
      created_at: account.created_at,
      updated_at: account.updated_at,
    };
  }

  function readBody(req) {
    return new Promise((done) => {
      let raw = "";
      req.on("data", (chunk) => (raw += chunk));
      req.on("end", () => {
        try {
          done(raw ? JSON.parse(raw) : {});
        } catch {
          done({});
        }
      });
    });
  }

  function connectPage(token, link) {
    const name = toolkitName(link.slug);
    const scheme = authConfigs.get(link.slug)?.scheme ?? "OAUTH2";
    const finish = (ok) => `/connect/finish?token=${encodeURIComponent(token)}&ok=${ok}`;
    const keyLabel = scheme === "BEARER_TOKEN" ? "Token" : scheme === "BASIC" ? "Password" : "API key";
    const body =
      scheme === "OAUTH2"
        ? `<h1>Multica wants to access your ${name} account</h1>
<p>In production this step is ${name}'s own consent screen, reached through Composio's connect link.</p>
<div class="row"><a class="no" id="deny" href="${finish(0)}">Cancel</a><a class="ok" id="allow" href="${finish(1)}">Allow</a></div>`
        : `<h1>Connect ${name}</h1>
<p>In production Composio's connect page asks for your ${name} ${scheme === "API_KEY" ? keyLabel : keyLabel.toLowerCase()}. The fake accepts anything.</p>
<form action="/connect/finish" method="get"><input type="hidden" name="token" value="${token}"/><input type="hidden" name="ok" value="1"/>
<label>${keyLabel}<input id="key" name="key" type="password" required placeholder="${keyLabel}"/></label>
<div class="row"><a class="no" id="deny" href="${finish(0)}">Cancel</a><button class="ok" id="allow" type="submit">Connect</button></div></form>`;
    return `<!doctype html><html><head><meta charset="utf-8"><title>Connect ${name}</title><style>
body{font-family:-apple-system,system-ui,sans-serif;background:#f5f5f7;margin:0;display:flex;align-items:center;justify-content:center;height:100vh}
.card{background:#fff;border-radius:16px;padding:36px 40px;width:420px;box-shadow:0 8px 30px rgba(0,0,0,.08)}
.tag{display:inline-block;background:#fff3cd;color:#7a5b00;font-size:12px;padding:3px 8px;border-radius:6px;margin-bottom:18px}
h1{font-size:20px;margin:0 0 8px}p{color:#555;font-size:14px;line-height:1.5}
.logo{width:44px;height:44px;border-radius:10px;margin-bottom:14px}
.row{display:flex;gap:10px;margin-top:24px}a,button{flex:1;text-align:center;padding:10px 0;border-radius:8px;border:0;text-decoration:none;font:600 14px inherit;cursor:pointer}
.ok{background:#111;color:#fff}.no{background:#eee;color:#333}
label{display:block;font-size:13px;font-weight:600;margin-top:16px}input[type=password]{display:block;box-sizing:border-box;width:100%;margin-top:6px;padding:9px 10px;border:1px solid #ccc;border-radius:8px;font-size:14px}</style></head><body><div class="card">
<div class="tag">Local stand-in for Composio's hosted connect page</div><br/>
<img class="logo" alt="" src="https://logos.composio.dev/api/${link.slug}"/>
${body}
</div></body></html>`;
  }

  async function route(req, res) {
    const url = new URL(req.url, baseURL);
    const path = url.pathname;
    const method = req.method;

    if (path === "/health") return send(res, 200, { ok: true });

    if (path === "/connect") {
      const token = url.searchParams.get("token") ?? "";
      const link = links.get(token);
      if (!link) return sendError(res, 404, "NOT_FOUND", "unknown connect link");
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      return res.end(connectPage(token, link));
    }
    if (path === "/connect/finish") {
      const link = links.get(url.searchParams.get("token") ?? "");
      if (!link) return sendError(res, 404, "NOT_FOUND", "unknown connect link");
      const callback = new URL(link.callback_url);
      if (url.searchParams.get("ok") === "1") {
        const now = new Date().toISOString();
        const config = authConfigs.get(link.slug);
        accounts.set(link.account_id, {
          id: link.account_id,
          user_id: link.user_id,
          alias: link.alias,
          auth_config_id: link.auth_config_id,
          scheme: config?.scheme ?? "OAUTH2",
          managed: Boolean(config?.managed),
          slug: link.slug,
          status: "ACTIVE",
          created_at: now,
          updated_at: now,
        });
        callback.searchParams.set("status", "success");
        callback.searchParams.set("connected_account_id", link.account_id);
      } else {
        callback.searchParams.set("status", "failed");
      }
      res.writeHead(302, { location: callback.toString() });
      return res.end();
    }

    if (!path.startsWith(`${API_PREFIX}/`)) return sendError(res, 404, "NOT_FOUND", "not found");
    const api = path.slice(API_PREFIX.length);

    if (method === "GET" && api === "/toolkits") {
      const category = url.searchParams.get("category");
      return send(res, 200, page(TOOLKITS.filter((t) => !category || t.category.id === category).map(listedToolkit)));
    }
    if (method === "GET" && api.startsWith("/toolkits/")) {
      const slug = decodeURIComponent(api.slice("/toolkits/".length)).toLowerCase();
      const toolkit = TOOLKITS.find((t) => t.slug === slug);
      if (!toolkit) return sendError(res, 404, "TOOLKIT_NOT_FOUND", "toolkit not found");
      return send(res, 200, toolkitDetails(toolkit));
    }
    if (method === "GET" && api === "/auth_configs") {
      const wanted = (url.searchParams.get("toolkit_slug") ?? "").split(",").filter(Boolean);
      const items = [...authConfigs.entries()]
        .filter(([slug]) => wanted.length === 0 || wanted.includes(slug))
        .map(([slug, config]) => authConfigView(slug, config));
      return send(res, 200, page(items));
    }
    if (method === "POST" && api === "/auth_configs") {
      const body = await readBody(req);
      const slug = body?.toolkit?.slug;
      const toolkit = TOOLKITS.find((t) => t.slug === slug);
      if (!toolkit) return sendError(res, 400, "INVALID_INPUT", "toolkit.slug must name a known toolkit");
      const custom = body.auth_config?.type === "use_custom_auth";
      const scheme = custom ? String(body.auth_config.authScheme ?? "").toUpperCase() : "OAUTH2";
      if (custom ? !toolkit.modes.includes(scheme) || scheme === "NO_AUTH" : !toolkit.managed) {
        return sendError(res, 400, "INVALID_INPUT", `${slug} does not offer ${custom ? scheme : "managed"} auth`);
      }
      const config = newAuthConfig(slug, scheme, !custom);
      authConfigs.set(slug, config);
      return send(res, 201, {
        toolkit: { slug },
        auth_config: { id: config.id, auth_scheme: scheme, is_composio_managed: config.managed, managed_app_id: config.managed ? `ma_${slug}` : null },
      });
    }
    if (method === "POST" && api === "/connected_accounts/link") {
      const body = await readBody(req);
      const slug = slugOfAuthConfig(body.auth_config_id);
      if (!slug) return sendError(res, 404, "AUTH_CONFIG_NOT_FOUND", "auth config not found");
      const token = `lk_${seq++}`;
      const accountID = `ca_${slug}_${seq++}`;
      links.set(token, { ...body, slug, account_id: accountID });
      return send(res, 201, {
        link_token: token,
        redirect_url: `${baseURL}/connect?token=${token}`,
        expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
        connected_account_id: accountID,
      });
    }
    if (method === "GET" && api === "/connected_accounts") {
      const filters = {
        id: url.searchParams.getAll("connected_account_ids"),
        user_id: url.searchParams.getAll("user_ids"),
        auth_config_id: url.searchParams.getAll("auth_config_ids"),
        status: url.searchParams.getAll("statuses"),
      };
      const slugs = url.searchParams.getAll("toolkit_slugs");
      const items = [...accounts.values()].filter(
        (account) =>
          Object.entries(filters).every(([key, values]) => values.length === 0 || values.includes(account[key])) &&
          (slugs.length === 0 || slugs.includes(account.slug)),
      );
      return send(res, 200, page(items.map(accountView)));
    }
    let match = /^\/connected_accounts\/([^/]+)\/revoke$/.exec(api);
    if (method === "POST" && match) {
      const account = accounts.get(decodeURIComponent(match[1]));
      if (!account) return sendError(res, 404, "CONNECTED_ACCOUNT_NOT_FOUND", "connected account not found");
      account.status = "INACTIVE";
      account.updated_at = new Date().toISOString();
      return send(res, 200, {
        revoked_tokens: account.scheme === "OAUTH2" ? ["access_token", "refresh_token"] : ["credential"],
        connected_account: { id: account.id, status: account.status },
      });
    }
    match = /^\/connected_accounts\/([^/]+)$/.exec(api);
    if (method === "DELETE" && match) {
      if (!accounts.delete(decodeURIComponent(match[1]))) {
        return sendError(res, 404, "CONNECTED_ACCOUNT_NOT_FOUND", "connected account not found");
      }
      return send(res, 200, { success: true });
    }
    if (method === "GET" && api === "/tools") {
      const slug = (url.searchParams.get("toolkit_slug") ?? "").toLowerCase();
      const search = (url.searchParams.get("search") ?? "").toLowerCase();
      const items = toolsOf(slug)
        .map((tool) => toolView(slug, tool))
        .filter((tool) => !search || `${tool.slug} ${tool.description}`.toLowerCase().includes(search));
      return send(res, 200, page(items));
    }
    if (method === "POST" && api.startsWith("/tools/execute/")) {
      const toolSlug = decodeURIComponent(api.slice("/tools/execute/".length)).toUpperCase();
      const body = await readBody(req);
      if (!findTool(toolSlug)) return sendError(res, 404, "TOOL_NOT_FOUND", "tool not found");
      // Accounts issued before a restart are unknown here but still live in
      // Multica's database; only an account revoked since is refused.
      const account = accounts.get(body.connected_account_id);
      if (account && account.status !== "ACTIVE") {
        return send(res, 200, { successful: false, error: "connected account is not active", data: {} });
      }
      log(`execute ${toolSlug} as ${body.connected_account_id ?? body.user_id ?? "?"}`);
      return send(res, 200, { successful: true, error: null, data: cannedResult(toolSlug, body.arguments ?? {}), log_id: `log_${seq++}` });
    }
    if (method === "GET" && api.startsWith("/tools/")) {
      const tool = findTool(decodeURIComponent(api.slice("/tools/".length)));
      return tool ? send(res, 200, tool) : sendError(res, 404, "TOOL_NOT_FOUND", "tool not found");
    }
    // An empty MCP URL tells Multica there is no session to attach.
    if (method === "POST" && api === "/tool_router/session") {
      const body = await readBody(req);
      return send(res, 201, {
        session_id: `trs_${seq++}`,
        mcp: { type: "http", url: "" },
        tool_router_tools: [],
        config: { user_id: String(body.user_id ?? ""), instant: false, preload: { tools: [] }, search: {}, execute: {} },
        config_version: 1,
      });
    }
    return sendError(res, 404, "NOT_FOUND", `the fake does not implement ${method} ${api}`);
  }

  const server = http.createServer((req, res) => {
    route(req, res).catch((error) => sendError(res, 500, "INTERNAL_SERVER_ERROR", String(error)));
    if (req.url !== "/health") log(`${req.method} ${req.url}`);
  });

  const stop = () => {
    if (stateFile && readState(stateFile)?.pid === process.pid) rmSync(stateFile, { force: true });
    process.exit(0);
  };
  for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, stop);

  server.on("error", (error) => fail(`cannot listen on ${HOST}:${port}: ${error.message}`));
  server.listen(port, HOST, () => {
    const actual = server.address().port;
    baseURL = `http://${HOST}:${actual}`;
    if (stateFile) {
      writeFileSync(stateFile, JSON.stringify({ pid: process.pid, port: actual, started_at: new Date().toISOString() }));
    }
    log(`serving ${baseURL}${API_PREFIX} (pid ${process.pid})`);
    if (!watchFile) {
      console.log(
        `\nPoint an API at it with:\n  COMPOSIO_API_KEY=${API_KEY}\n  COMPOSIO_BASE_URL=${baseURL}${API_PREFIX}\n` +
          "  FF_COMPOSIO_MCP_APPS=true\n  COMPOSIO_STATE_SECRET=<any string>   # unless JWT_SECRET is set",
      );
    }
  });

  if (watchFile) {
    setInterval(() => {
      if (!existsSync(watchFile)) {
        log(`${watchFile} is gone; the environment was destroyed, exiting.`);
        stop();
      }
    }, 5_000).unref();
  }
}

// -------------------------------------------------------------- environment ---

function readState(file) {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return null;
  }
}

function isFakeProcess(pid) {
  if (!pid) return false;
  const ps = spawnSync("ps", ["-p", String(pid), "-o", "command="], { encoding: "utf8" });
  return ps.status === 0 && ps.stdout.includes("fake-composio.mjs") && ps.stdout.includes(" serve");
}

async function healthy(port) {
  try {
    const response = await fetch(`http://${HOST}:${port}/health`, { signal: AbortSignal.timeout(2_000) });
    return response.ok;
  } catch {
    return false;
  }
}

function devEnv(...args) {
  return spawnSync("bash", [join(REPO_ROOT, "scripts/dev-env.sh"), ...args], { cwd: REPO_ROOT, encoding: "utf8" });
}

// `status` exits non-zero when a component is not running, which is fine here:
// only the environment's paths and the API's state are read. Without an
// environment it returns null when optional, and fails otherwise.
function environment({ optional = false } = {}) {
  const result = devEnv("status", "--json");
  let status;
  try {
    status = JSON.parse(result.stdout);
  } catch {
    if (optional) return null;
    fail(`this checkout has no \`make up\` environment; run \`make up\` first.\n${result.stderr.trim()}`);
  }
  const stateDir = dirname(status.logs);
  return {
    name: status.name,
    envFile: isAbsolute(status.env_file) ? status.env_file : join(status.dir, status.env_file),
    logs: status.logs,
    manifest: join(stateDir, "manifest.env"),
    stateFile: join(stateDir, "fake-composio.json"),
    logFile: join(status.logs, "fake-composio.log"),
    apiRunning: ["running", "stale"].includes(status.components?.api?.state),
    components: Object.entries(status.components ?? {})
      .filter(([, component]) => component.state !== "stopped")
      .map(([name]) => name),
  };
}

function splitBlock(text) {
  const begin = text.indexOf(BLOCK_BEGIN);
  if (begin === -1) return { rest: text, block: "" };
  const endMarker = text.indexOf(BLOCK_END, begin);
  const end = endMarker === -1 ? text.length : endMarker + BLOCK_END.length + 1;
  return { rest: text.slice(0, begin) + text.slice(end), block: text.slice(begin, end) };
}

function blockValue(block, key) {
  return new RegExp(`^${key}=(.*)$`, "m").exec(block)?.[1] ?? "";
}

function renderBlock(port, stateSecret) {
  return [
    BLOCK_BEGIN,
    `COMPOSIO_API_KEY=${API_KEY}`,
    `COMPOSIO_BASE_URL=http://${HOST}:${port}${API_PREFIX}`,
    `COMPOSIO_STATE_SECRET=${stateSecret}`,
    "FF_COMPOSIO_MCP_APPS=true",
    BLOCK_END,
    "",
  ].join("\n");
}

function restartAPI() {
  log("restarting the API so it reads the new settings");
  for (const args of [["down", "--components", "api"], ["up", "--components", "api"]]) {
    const result = spawnSync("bash", [join(REPO_ROOT, "scripts/dev-env.sh"), ...args], { cwd: REPO_ROOT, stdio: "inherit" });
    if (result.status !== 0) fail(`\`dev-env.sh ${args.join(" ")}\` failed; see its output above.`);
  }
}

async function up() {
  const env = environment();
  if (!existsSync(env.envFile)) fail(`${env.envFile} does not exist; run \`make up\` first.`);

  let state = readState(env.stateFile);
  if (!(state && isFakeProcess(state.pid) && (await healthy(state.port)))) {
    if (state && isFakeProcess(state.pid)) process.kill(state.pid, "SIGTERM");
    rmSync(env.stateFile, { force: true });
    // Keep the port the env file already names, so a running API stays valid;
    // otherwise let the OS pick a free one.
    const previous = Number(/:(\d+)\//.exec(blockValue(splitBlock(readFileSync(env.envFile, "utf8")).block, "COMPOSIO_BASE_URL"))?.[1]);
    const port = previous && (await portFree(previous)) ? previous : 0;
    mkdirSync(env.logs, { recursive: true });
    const out = openSync(env.logFile, "a");
    const child = spawn(
      process.execPath,
      [SCRIPT, "serve", "--port", String(port), "--state", env.stateFile, "--watch", env.manifest],
      { cwd: REPO_ROOT, detached: true, stdio: ["ignore", out, out] },
    );
    child.unref();
    const deadline = Date.now() + 10_000;
    for (;;) {
      state = readState(env.stateFile);
      if (state?.pid === child.pid && (await healthy(state.port))) break;
      if (child.exitCode !== null || Date.now() > deadline) {
        fail(`the fake did not start; see ${env.logFile}\n${readFileSync(env.logFile, "utf8").split("\n").slice(-10).join("\n")}`);
      }
      await sleep(200);
    }
    log(`started on ${HOST}:${state.port} (pid ${state.pid}), log: ${env.logFile}`);
  } else {
    log(`already running on ${HOST}:${state.port} (pid ${state.pid})`);
  }

  const { rest, block } = splitBlock(readFileSync(env.envFile, "utf8"));
  const stateSecret = blockValue(block, "COMPOSIO_STATE_SECRET") || randomBytes(32).toString("hex");
  const wanted = renderBlock(state.port, stateSecret);
  if (block !== wanted) {
    writeFileSync(env.envFile, `${rest.replace(/\n*$/, "\n")}\n${wanted}`);
    log(`wrote COMPOSIO_API_KEY, COMPOSIO_BASE_URL, COMPOSIO_STATE_SECRET and FF_COMPOSIO_MCP_APPS to ${env.envFile}`);
    if (env.apiRunning) restartAPI();
    else log("the API is not running; the next `make up` starts it pointed at the fake.");
  }
  log(`ready: ${env.name} uses the fake Composio at http://${HOST}:${state.port}${API_PREFIX}`);
}

function portFree(port) {
  return new Promise((done) => {
    const probe = http.createServer();
    probe.once("error", () => done(false));
    probe.listen(port, HOST, () => probe.close(() => done(true)));
  });
}

// After `make destroy` there is no environment left to ask, and the fake has
// already exited; the block can still be in the checkout's env file.
async function down() {
  const env = environment({ optional: true });
  if (env) {
    const state = readState(env.stateFile);
    if (state && isFakeProcess(state.pid)) {
      process.kill(state.pid, "SIGTERM");
      log(`stopped pid ${state.pid}`);
    } else {
      log("not running");
    }
    rmSync(env.stateFile, { force: true });
  }
  const envFile = env?.envFile ?? [".env", ".env.worktree"].map((name) => join(REPO_ROOT, name)).find(existsSync);
  if (!envFile || !existsSync(envFile)) return;
  const { rest, block } = splitBlock(readFileSync(envFile, "utf8"));
  if (!block) return;
  writeFileSync(envFile, rest.replace(/\n+$/, "\n"));
  log(`removed the fake's settings from ${envFile}`);
  if (env?.apiRunning) {
    log(`the running API still points at the fake until it restarts: make down C=api && make up C=${env.components.join(",")}`);
  }
}

async function status() {
  const env = environment();
  const state = readState(env.stateFile);
  const running = state && isFakeProcess(state.pid) && (await healthy(state.port));
  const block = existsSync(env.envFile) ? splitBlock(readFileSync(env.envFile, "utf8")).block : "";
  console.log(`environment  ${env.name}`);
  console.log(`fake         ${running ? `running on http://${HOST}:${state.port}${API_PREFIX} (pid ${state.pid})` : "stopped"}`);
  console.log(`env file     ${block ? `points at ${blockValue(block, "COMPOSIO_BASE_URL")}` : "has no fake-composio settings"}`);
  console.log(`log          ${env.logFile}`);
  if (running !== Boolean(block)) console.log("\nRun `node scripts/fake-composio.mjs up` (or `down`) to make them agree.");
}

// --------------------------------------------------------------------- main ---

function option(args, name) {
  const index = args.indexOf(name);
  return index === -1 ? undefined : args[index + 1];
}

const [command, ...args] = process.argv.slice(2);
switch (command) {
  case "up":
    await up();
    break;
  case "down":
    await down();
    break;
  case "status":
    await status();
    break;
  case "serve":
    serve({ port: Number(option(args, "--port") ?? 0), stateFile: option(args, "--state"), watchFile: option(args, "--watch") });
    break;
  case "-h":
  case "--help":
    console.log(USAGE);
    break;
  default:
    fail(`unknown command ${command ?? "(none)"}\n\n${USAGE}`);
}
