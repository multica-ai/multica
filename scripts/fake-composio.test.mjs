// Runs scripts/fake-composio.mjs and checks every response it gives the
// requests server/pkg/composio makes against the snapshot of Composio's v3.1
// OpenAPI spec (see scripts/composio-spec.mjs).
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { fileURLToPath } from "node:url";

import { API_PREFIX, OPERATIONS, check, loadSnapshot, responseSchema, successStatus } from "./composio-spec.mjs";

const spec = loadSnapshot();
const checked = new Set();
let fake;
let origin;

before(async () => {
  fake = spawn(process.execPath, [fileURLToPath(new URL("./fake-composio.mjs", import.meta.url)), "serve"], {
    stdio: ["ignore", "pipe", "inherit"],
  });
  let output = "";
  fake.stdout.setEncoding("utf8");
  origin = await new Promise((resolve, reject) => {
    fake.stdout.on("data", (chunk) => {
      output += chunk;
      const serving = /serving (http:\/\/[\d.]+:\d+)/.exec(output);
      if (serving) resolve(serving[1]);
    });
    fake.on("exit", () => reject(new Error(`the fake exited before serving:\n${output}`)));
  });
});

after(async () => {
  if (fake.exitCode === null) {
    fake.kill("SIGTERM");
    await once(fake, "exit");
  }
});

// call sends a request the way the Go client does and fails the test unless
// the answer matches op's response schema for its status.
async function call(op, path, { method = op.split(" ")[0], body, status = successStatus(spec, op) } = {}) {
  const response = await fetch(`${origin}${API_PREFIX}${path}`, {
    method,
    headers: { "content-type": "application/json", "x-api-key": "fake-composio" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const json = await response.json();
  assert.equal(response.status, status, `${method} ${path} answered ${response.status}: ${JSON.stringify(json)}`);
  const schema = responseSchema(spec, op, response.status);
  assert.ok(schema, `the spec declares no ${response.status} response for ${op}`);
  assert.deepEqual(check(spec, schema, json), [], `${method} ${path} does not match ${op} ${response.status}`);
  checked.add(`${op} ${response.status}`);
  return json;
}

// connect runs the hosted connect flow for an auth config and returns the new
// connected account id.
async function connect(authConfigID, userID) {
  const link = await call("POST /connected_accounts/link", "/connected_accounts/link", {
    body: { auth_config_id: authConfigID, user_id: userID, callback_url: "http://127.0.0.1:1/callback" },
  });
  const finish = await fetch(`${origin}/connect/finish?token=${link.link_token}&ok=1`, { redirect: "manual" });
  const callback = new URL(finish.headers.get("location"));
  assert.equal(callback.searchParams.get("status"), "success");
  assert.equal(callback.searchParams.get("connected_account_id"), link.connected_account_id);
  return link.connected_account_id;
}

test("toolkits match the spec, with categories under meta and auth schemes only in the list", async () => {
  const listed = await call("GET /toolkits", "/toolkits?limit=1000&sort_by=usage");
  const github = listed.items.find((t) => t.slug === "github");
  assert.deepEqual(github.meta.categories, [{ id: "developer-tools-devops", name: "Developer Tools & DevOps" }]);
  await call("GET /toolkits", "/toolkits?category=developer-tools-devops");

  for (const slug of ["gmail", "posthog", "exa", "hackernews"]) {
    const details = await call("GET /toolkits/{slug}", `/toolkits/${slug}`);
    assert.ok(details.auth_config_details.length > 0, `${slug} lists no auth_config_details`);
  }
  await call("GET /toolkits/{slug}", "/toolkits/nope", { status: 404 });
});

test("auth configs, connect links and connected accounts match the spec", async () => {
  await call("GET /auth_configs", "/auth_configs?toolkit_slug=gmail,slack");
  await call("POST /auth_configs", "/auth_configs", {
    body: { toolkit: { slug: "slack" }, auth_config: { type: "use_composio_managed_auth" } },
  });
  const created = await call("POST /auth_configs", "/auth_configs", {
    body: { toolkit: { slug: "posthog" }, auth_config: { type: "use_custom_auth", authScheme: "API_KEY", name: "PostHog", credentials: {} } },
  });
  await call("POST /auth_configs", "/auth_configs", {
    body: { toolkit: { slug: "posthog" }, auth_config: { type: "use_custom_auth", authScheme: "BASIC", credentials: {} } },
    status: 400,
  });
  await call("POST /auth_configs", "/auth_configs", {
    body: { toolkit: { slug: "exa" }, auth_config: { type: "use_custom_auth", authScheme: "BEARER_TOKEN", credentials: {} } },
  });
  await call("GET /auth_configs", "/auth_configs");

  const keyAccount = await connect(created.auth_config.id, "u_1");
  const oauthAccount = await connect("ac_gmail", "u_1");
  const tokenAccount = await connect("ac_exa", "u_2");
  await call("POST /connected_accounts/link", "/connected_accounts/link", {
    body: { auth_config_id: "ac_missing", user_id: "u_1" },
    status: 404,
  });

  const listed = await call("GET /connected_accounts", `/connected_accounts?user_ids=u_1&user_ids=u_2&statuses=ACTIVE`);
  assert.deepEqual(listed.items.map((a) => a.id).sort(), [keyAccount, oauthAccount, tokenAccount].sort());
  const [account] = (await call("GET /connected_accounts", `/connected_accounts?connected_account_ids=${oauthAccount}`)).items;
  assert.equal(account.auth_config.id, "ac_gmail");

  for (const id of [oauthAccount, keyAccount, tokenAccount]) {
    await call("POST /connected_accounts/{nanoid}/revoke", `/connected_accounts/${id}/revoke`, { body: {} });
  }
  await call("GET /connected_accounts", `/connected_accounts?connected_account_ids=${oauthAccount}&connected_account_ids=${tokenAccount}`);
  await call("POST /tools/execute/{tool_slug}", "/tools/execute/GMAIL_FETCH_EMAILS", {
    body: { arguments: {}, connected_account_id: oauthAccount, user_id: "u_1", version: "20250905_00" },
  });
  await call("DELETE /connected_accounts/{nanoid}", `/connected_accounts/${keyAccount}`);
  await call("DELETE /connected_accounts/{nanoid}", `/connected_accounts/${keyAccount}`, { status: 404 });
  await call("POST /connected_accounts/{nanoid}/revoke", `/connected_accounts/${keyAccount}/revoke`, { body: {}, status: 404 });
});

test("tools, tool execution and tool-router sessions match the spec", async () => {
  await call("GET /tools", "/tools?toolkit_slug=gmail&toolkit_versions=latest");
  await call("GET /tools", "/tools?toolkit_slug=asana&search=record&limit=50&toolkit_versions=latest");
  await call("GET /tools/{tool_slug}", "/tools/GMAIL_SEND_EMAIL?toolkit_versions=latest");
  await call("GET /tools/{tool_slug}", "/tools/NOPE?toolkit_versions=latest", { status: 404 });

  const oauthAccount = await connect("ac_gmail", "u_3");
  await call("POST /tools/execute/{tool_slug}", "/tools/execute/GMAIL_FETCH_EMAILS", {
    body: { arguments: {}, connected_account_id: oauthAccount, user_id: "u_3", version: "20250905_00" },
  });
  await call("POST /tools/execute/{tool_slug}", "/tools/execute/SLACK_SEND_MESSAGE", {
    body: { arguments: { query: "hi" }, user_id: "u_3" },
  });
  await call("POST /tools/execute/{tool_slug}", "/tools/execute/NOPE", { body: { arguments: {}, user_id: "u_3" }, status: 404 });

  await call("POST /tool_router/session", "/tool_router/session", {
    body: { user_id: "u_3", manage_connections: { enable: false } },
  });
});

// Runs last: node:test runs a file's top-level tests in order.
test("every operation the Go client calls was checked", () => {
  const missing = OPERATIONS.filter((op) => !checked.has(`${op} ${successStatus(spec, op)}`));
  assert.deepEqual(missing, [], "add a call for each operation to this test");
});

// The two shapes that hid MUL-9920's bugs, so the check itself cannot quietly
// stop catching them.
test("the check rejects the fields Composio does not return", async () => {
  const listSchema = responseSchema(spec, "GET /toolkits", 200);
  const detailSchema = responseSchema(spec, "GET /toolkits/{slug}", 200);
  const listed = await (await fetch(`${origin}${API_PREFIX}/toolkits`)).json();
  const details = await (await fetch(`${origin}${API_PREFIX}/toolkits/posthog`)).json();

  const topLevelCategories = structuredClone(listed);
  topLevelCategories.items[0].categories = ["Data & Analytics"];
  assert.match(check(spec, listSchema, topLevelCategories).join("\n"), /\$\.items\[0\]\.categories: is not in the spec/);

  for (const field of ["auth_schemes", "no_auth"]) {
    const withListFields = { ...details, [field]: field === "no_auth" ? false : ["api_key"] };
    assert.match(check(spec, detailSchema, withListFields).join("\n"), new RegExp(`\\$\\.${field}: is not in the spec`));
  }

  const { auth_config_details: _, ...withoutDetails } = details;
  assert.deepEqual(check(spec, detailSchema, withoutDetails), [], "auth_config_details is optional in the spec");
  const missingMeta = { ...details, meta: { ...details.meta, categories: undefined } };
  assert.match(check(spec, detailSchema, JSON.parse(JSON.stringify(missingMeta))).join("\n"), /\$\.meta: misses required field categories/);
});

test("fields mode skips required fields and the zero values of unset Go fields", () => {
  const schema = responseSchema(spec, "GET /toolkits/{slug}", 200);
  assert.deepEqual(check(spec, schema, { slug: "notion", auth_schemes: null, no_auth: false }, { fields: true }), []);
  assert.deepEqual(check(spec, schema, { slug: "notion", auth_schemes: ["oauth2"] }, { fields: true }), [
    "$.auth_schemes: is not in the spec",
  ]);
  assert.deepEqual(check(spec, schema, { slug: 1 }, { fields: true }), ["$.slug: is integer, the spec says string"]);
});
