import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";

const SCRIPT_PATH = new URL("./auto-pin-nonterminal-issues.user.js", import.meta.url);
const BASE_URL = "https://app.multica.ai/";

function loadApi() {
  const sandbox = {
    URL,
    clearTimeout,
    console,
    decodeURIComponent,
    encodeURIComponent,
    JSON,
    Promise,
    setTimeout,
  };
  sandbox.globalThis = sandbox;
  sandbox.window = sandbox;
  vm.createContext(sandbox);

  if (existsSync(SCRIPT_PATH)) {
    vm.runInContext(readFileSync(SCRIPT_PATH, "utf8"), sandbox, {
      filename: SCRIPT_PATH.pathname,
    });
  }

  return sandbox.MulticaAutoPin ?? {};
}

function requireApi() {
  const api = loadApi();
  assert.equal(typeof api.createController, "function");
  assert.equal(typeof api.install, "function");
  return api;
}

function response(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async json() {
      return body;
    },
  };
}

function requestRecord(input, init = {}) {
  const requestUrl = new URL(
    typeof input === "string" ? input : input.url,
    BASE_URL,
  );
  const method = (init.method ?? input.method ?? "GET").toUpperCase();
  let body = init.body;
  if (typeof body === "string") {
    try {
      body = JSON.parse(body);
    } catch {
      // Keep non-JSON bodies as-is for assertions that need the raw value.
    }
  }
  return {
    body,
    credentials: init.credentials,
    headers: init.headers,
    method,
    path: `${requestUrl.pathname}${requestUrl.search}`,
  };
}

function makeFetch(routes, calls) {
  return async (input, init = {}) => {
    const request = requestRecord(input, init);
    calls.push(request);
    const route = routes.find(
      (candidate) =>
        candidate.method === request.method && candidate.path === request.path,
    );
    if (!route) {
      throw new Error(`Unexpected request: ${request.method} ${request.path}`);
    }
    return typeof route.response === "function"
      ? route.response(request)
      : route.response;
  };
}

function controllerOptions(fetchImpl, extra = {}) {
  return {
    debounceMs: 0,
    fetchImpl,
    schedule: () => 1,
    target: { location: { href: BASE_URL } },
    ...extra,
  };
}

async function settle(controller) {
  await controller.waitForIdle();
  await Promise.resolve();
}

test("exposes a controller API for the browser userscript", () => {
  const api = loadApi();
  assert.equal(typeof api.createController, "function");
  assert.equal(typeof api.install, "function");
});

test("reconciles all open issues and removes stale terminal issue pins", async () => {
  const api = requireApi();
  const calls = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "GET",
        path: "/api/issues?open_only=true",
        response: response({
          issues: [
            { id: "open-1", status: "todo" },
            { id: "open-2", status: "in_progress" },
            { id: "defensive-done", status: "done" },
          ],
          total: 3,
        }),
      },
      {
        method: "GET",
        path: "/api/pins",
        response: response([
          { item_type: "issue", item_id: "open-2" },
          { item_type: "issue", item_id: "terminal-1" },
          { item_type: "project", item_id: "project-1" },
        ]),
      },
      {
        method: "POST",
        path: "/api/pins",
        response: (request) => {
          assert.deepEqual(request.body, {
            item_type: "issue",
            item_id: "open-1",
          });
          return response({ id: "new-pin" }, 201);
        },
      },
      {
        method: "DELETE",
        path: "/api/pins/issue/terminal-1",
        response: response(undefined, 204),
      },
    ],
    calls,
  );
  const controller = api.createController(controllerOptions(fetchImpl));

  await controller.reconcile();

  assert.deepEqual(
    calls.map(({ method, path }) => `${method} ${path}`),
    [
      "GET /api/issues?open_only=true",
      "GET /api/pins",
      "POST /api/pins",
      "DELETE /api/pins/issue/terminal-1",
    ],
  );
});

test("fails closed when the open-issue response is malformed", async () => {
  const api = requireApi();
  const calls = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "GET",
        path: "/api/issues?open_only=true",
        response: response({ issues: [{ id: "issue-unsafe", status: "todo" }] }),
      },
      {
        method: "GET",
        path: "/api/pins",
        response: response([{ item_type: "issue", item_id: "issue-unsafe" }]),
      },
    ],
    calls,
  );
  const controller = api.createController(controllerOptions(fetchImpl));

  await controller.reconcile();

  assert.deepEqual(
    calls.map(({ method, path }) => `${method} ${path}`),
    ["GET /api/issues?open_only=true", "GET /api/pins"],
  );
});

test("manual issue unpin updates the issue to done without re-entering the interceptor", async () => {
  const api = requireApi();
  const calls = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "DELETE",
        path: "/api/pins/issue/issue-1",
        response: response(undefined, 204),
      },
      {
        method: "PUT",
        path: "/api/issues/issue-1",
        response: (request) => {
          assert.deepEqual(request.body, { status: "done" });
          assert.equal(request.credentials, "include");
          assert.equal(request.headers["X-CSRF-Token"], "csrf-token");
          assert.equal(request.headers["X-Workspace-Slug"], "team");
          return response({ id: "issue-1", status: "done" });
        },
      },
    ],
    calls,
  );
  const controller = api.createController(
    controllerOptions(fetchImpl, {
      target: {
        document: { cookie: "multica_csrf=csrf-token" },
        location: {
          href: `${BASE_URL}team/issues`,
          pathname: "/team/issues",
        },
      },
    }),
  );
  const wrappedFetch = controller.wrapFetch();

  const unpinResponse = await wrappedFetch("/api/pins/issue/issue-1", {
    method: "DELETE",
  });
  await settle(controller);

  assert.equal(unpinResponse.status, 204);
  assert.deepEqual(
    calls.map(({ method, path }) => `${method} ${path}`),
    ["DELETE /api/pins/issue/issue-1", "PUT /api/issues/issue-1"],
  );
});

test("successful comments move any non-in-progress issue to in_progress", async () => {
  const api = requireApi();
  const calls = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "POST",
        path: "/api/issues/issue-2/comments",
        response: response({ id: "comment-1" }, 201),
      },
      {
        method: "GET",
        path: "/api/issues/issue-2",
        response: response({ id: "issue-2", status: "done" }),
      },
      {
        method: "PUT",
        path: "/api/issues/issue-2",
        response: (request) => {
          assert.deepEqual(request.body, { status: "in_progress" });
          return response({ id: "issue-2", status: "in_progress" });
        },
      },
    ],
    calls,
  );
  const controller = api.createController(controllerOptions(fetchImpl));
  const wrappedFetch = controller.wrapFetch();

  const commentResponse = await wrappedFetch("/api/issues/issue-2/comments", {
    method: "POST",
    body: JSON.stringify({ content: "reopen this" }),
  });
  await settle(controller);

  assert.equal(commentResponse.status, 201);
  assert.deepEqual(
    calls.map(({ method, path }) => `${method} ${path}`),
    [
      "POST /api/issues/issue-2/comments",
      "GET /api/issues/issue-2",
      "PUT /api/issues/issue-2",
    ],
  );
});

test("does not update an issue that is already in progress after a comment", async () => {
  const api = requireApi();
  const calls = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "POST",
        path: "/api/issues/issue-3/comments",
        response: response({ id: "comment-2" }, 201),
      },
      {
        method: "GET",
        path: "/api/issues/issue-3",
        response: response({ id: "issue-3", status: "in_progress" }),
      },
    ],
    calls,
  );
  const controller = api.createController(controllerOptions(fetchImpl));
  await controller.wrapFetch()("/api/issues/issue-3/comments", {
    method: "POST",
  });
  await settle(controller);

  assert.deepEqual(
    calls.map(({ method, path }) => `${method} ${path}`),
    ["POST /api/issues/issue-3/comments", "GET /api/issues/issue-3"],
  );
});

test("ignores failed mutations and never creates a request loop", async () => {
  const api = requireApi();
  const calls = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "DELETE",
        path: "/api/pins/issue/issue-4",
        response: response({ error: "not allowed" }, 403),
      },
      {
        method: "POST",
        path: "/api/issues/issue-4/comments",
        response: response({ error: "not allowed" }, 500),
      },
    ],
    calls,
  );
  const controller = api.createController(controllerOptions(fetchImpl));
  const wrappedFetch = controller.wrapFetch();

  await wrappedFetch("/api/pins/issue/issue-4", { method: "DELETE" });
  await wrappedFetch("/api/issues/issue-4/comments", { method: "POST" });
  await settle(controller);

  assert.deepEqual(
    calls.map(({ method, path }) => `${method} ${path}`),
    [
      "DELETE /api/pins/issue/issue-4",
      "POST /api/issues/issue-4/comments",
    ],
  );
});

test("install patches fetch once and schedules reconciliation after issue-list reads", async () => {
  const api = requireApi();
  const calls = [];
  const scheduled = [];
  const fetchImpl = makeFetch(
    [
      {
        method: "GET",
        path: "/api/issues",
        response: response({ issues: [], total: 0 }),
      },
    ],
    calls,
  );
  const target = { fetch: fetchImpl, location: { href: BASE_URL } };
  const controller = api.install(target, {
    schedule: (callback) => {
      scheduled.push(callback);
      return scheduled.length;
    },
  });

  assert.equal(target.fetch, controller.fetch);
  assert.equal(api.install(target), controller);
  await target.fetch("/api/issues");

  // The initial page-load scan and the app's own list read share one
  // debounced scan; the script must not fan out another request per read.
  assert.equal(scheduled.length, 1);
  assert.deepEqual(calls.map(({ method, path }) => `${method} ${path}`), [
    "GET /api/issues",
  ]);
});
