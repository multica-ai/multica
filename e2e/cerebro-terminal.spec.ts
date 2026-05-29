// End-to-end test for the cerebro interactive-terminal feature.
//
// This spec talks directly to the running backend (no UI required) — it
// proves that the HTTP + WebSocket surface, the broker, the
// presentation-mode column, and the workspace-membership gate all work
// together against real Postgres. The frontend pieces (xterm panel,
// presentation-mode toggle) are exercised separately by the
// `@multica/cerebro-terminal` vitest + the runtime-detail view tests.

import { test, expect } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_NAME = "E2E Terminal User";
const DEFAULT_E2E_EMAIL = "e2e-terminal@multica.ai";
const DEFAULT_E2E_WORKSPACE = "e2e-terminal-ws";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

function wsUrlFor(attachPath: string): string {
  return `${API_BASE.replace(/^http/, "ws")}${attachPath}`;
}

interface RuntimeFixture {
  runtimeId: string;
  workspaceId: string;
  workspaceSlug: string;
  token: string;
}

async function seedInteractiveRuntime(): Promise<RuntimeFixture> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  const workspace = await api.ensureWorkspace(
    "E2E Terminal Workspace",
    DEFAULT_E2E_WORKSPACE,
  );
  const token = api.getToken();
  if (!token) throw new Error("login did not return a token");

  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    const tag = `term-e2e-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
    const inserted = await client.query<{ id: string }>(
      `INSERT INTO agent_runtime
         (workspace_id, daemon_id, name, runtime_mode, provider, status, presentation_mode)
       VALUES ($1, $2, $3, 'local', 'e2e', 'online', 'interactive')
       RETURNING id`,
      [workspace.id, tag, `Terminal E2E runtime ${tag}`],
    );
    const runtimeId = inserted.rows[0]!.id;
    return {
      runtimeId,
      workspaceId: workspace.id,
      workspaceSlug: workspace.slug,
      token,
    };
  } finally {
    await client.end();
  }
}

function workspaceHeaders(slug: string, token: string): Record<string, string> {
  return {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
    "X-Workspace-Slug": slug,
  };
}

async function deleteRuntime(runtimeId: string) {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    await client.query("DELETE FROM agent_runtime WHERE id = $1", [runtimeId]);
  } finally {
    await client.end();
  }
}

// Wait for `predicate(buffer)` to become true. Resolves with the buffer or
// rejects after `timeoutMs`. Polled by the WS message-collector below.
function waitFor(
  predicate: () => boolean,
  timeoutMs: number,
  label: string,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const t = setInterval(() => {
      if (predicate()) {
        clearInterval(t);
        resolve();
      } else if (Date.now() - start > timeoutMs) {
        clearInterval(t);
        reject(new Error(`timeout waiting for ${label} after ${timeoutMs}ms`));
      }
    }, 25);
  });
}

interface AttachState {
  ws: WebSocket;
  stdout: string;
  exitCode: number | null;
}

async function attach(
  token: string,
  workspaceSlug: string,
  attachPath: string,
): Promise<AttachState> {
  // Node's native WebSocket constructor (v22+) doesn't yet support the
  // 4-arg form with custom headers. Wrap the bearer in a Sec-WebSocket-
  // Protocol value the backend can parse as auth, or fall back to a query
  // param. Server middleware accepts Authorization: Bearer header on the
  // upgrade request through Sec-WebSocket-Protocol's auth-ish parameter
  // pattern… too fragile. Easier: rely on the cookie set by login. We
  // verified the bearer token works via authedFetch; let's pass it through
  // a query string the server can read.
  //
  // In this codebase the WS endpoint sits under the same auth subtree as
  // the REST routes, which look for the bearer in the Authorization
  // header. Node's WebSocket does accept headers via the
  // `WebSocket(url, [], { headers })` non-standard option in undici. Use
  // it directly.
  const ws = new WebSocket(wsUrlFor(attachPath), {
    headers: {
      Authorization: `Bearer ${token}`,
      "X-Workspace-Slug": workspaceSlug,
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as unknown as string[]);

  const state: AttachState = { ws, stdout: "", exitCode: null };

  await new Promise<void>((resolve, reject) => {
    ws.addEventListener("open", () => resolve(), { once: true });
    ws.addEventListener("error", (ev) => reject(new Error(`ws error: ${ev}`)), {
      once: true,
    });
  });

  ws.addEventListener("message", (ev) => {
    let frame: { type: string; data?: string; code?: number; error?: string };
    try {
      frame = JSON.parse(typeof ev.data === "string" ? ev.data : "");
    } catch {
      return;
    }
    if (frame.type === "stdout" && frame.data) {
      state.stdout += Buffer.from(frame.data, "base64").toString("utf8");
    } else if (frame.type === "exit") {
      state.exitCode = frame.code ?? 0;
    }
  });

  return state;
}

function sendStdin(ws: WebSocket, input: string) {
  ws.send(
    JSON.stringify({
      type: "stdin",
      data: Buffer.from(input, "utf8").toString("base64"),
    }),
  );
}

test.describe("cerebro interactive terminal", () => {
  let runtimeId: string | null = null;

  test.afterEach(async () => {
    if (runtimeId) {
      await deleteRuntime(runtimeId).catch(() => {});
      runtimeId = null;
    }
  });

  test("HTTP create + WS attach roundtrips stdin/stdout against a real backend", async () => {
    const { runtimeId: rid, workspaceSlug, token } = await seedInteractiveRuntime();
    runtimeId = rid;

    // 1. POST creates a session backed by a real child process.
    const createRes = await fetch(`${API_BASE}/api/cerebro/terminal/sessions`, {
      method: "POST",
      headers: workspaceHeaders(workspaceSlug, token),
      body: JSON.stringify({
        runtime_id: rid,
        command: ["sh", "-c", "echo hello-from-server; read x; echo got:$x"],
      }),
    });
    expect(createRes.status).toBe(201);
    const session = (await createRes.json()) as {
      id: string;
      runtime_id: string;
      attach_path: string;
    };
    expect(session.runtime_id).toBe(rid);
    expect(session.attach_path).toMatch(
      /^\/api\/cerebro\/terminal\/sessions\/.+\/ws$/,
    );

    // 2. Attach the WebSocket; collect stdout.
    const state = await attach(token, workspaceSlug, session.attach_path);

    // 3. Wait for the initial 'hello-from-server' to appear.
    await waitFor(
      () => state.stdout.includes("hello-from-server"),
      4000,
      "initial output",
    );
    expect(state.stdout).toContain("hello-from-server");

    // 4. Send 'world\n' as stdin.
    sendStdin(state.ws, "world\n");

    // 5. Wait for the echoed 'got:world' — proves stdin reached the child.
    await waitFor(
      () => state.stdout.includes("got:world"),
      4000,
      "stdin roundtrip",
    );
    expect(state.stdout).toContain("got:world");

    // 6. Child exits naturally; server should send an exit frame.
    await waitFor(() => state.exitCode !== null, 4000, "exit frame");
    expect(state.exitCode).toBe(0);

    state.ws.close();
  });

  test("POST sessions rejects headless runtime with 409", async () => {
    const { runtimeId: rid, workspaceSlug, token } = await seedInteractiveRuntime();
    runtimeId = rid;
    // Flip back to headless via the cerebro endpoint so we exercise that path too.
    const setRes = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/presentation-mode`,
      {
        method: "PUT",
        headers: workspaceHeaders(workspaceSlug, token),
        body: JSON.stringify({ presentation_mode: "headless" }),
      },
    );
    expect(setRes.status).toBe(200);

    const createRes = await fetch(`${API_BASE}/api/cerebro/terminal/sessions`, {
      method: "POST",
      headers: workspaceHeaders(workspaceSlug, token),
      body: JSON.stringify({ runtime_id: rid }),
    });
    expect(createRes.status).toBe(409);
  });

  test("GET runtimes/{id}/session returns 204 then 200 once a session exists", async () => {
    const { runtimeId: rid, workspaceSlug, token } = await seedInteractiveRuntime();
    runtimeId = rid;

    // 1. No session yet — GET returns 204 No Content.
    const empty = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/session`,
      { headers: workspaceHeaders(workspaceSlug, token) },
    );
    expect(empty.status).toBe(204);

    // 2. Create a session (any path — POST /sessions works). The broker
    //    tracks every open session by runtime_id, regardless of whether it
    //    was internally spawned (Create) or externally adopted (Adopt).
    const createRes = await fetch(`${API_BASE}/api/cerebro/terminal/sessions`, {
      method: "POST",
      headers: workspaceHeaders(workspaceSlug, token),
      body: JSON.stringify({
        runtime_id: rid,
        command: ["sh", "-c", "sleep 5"],
      }),
    });
    expect(createRes.status).toBe(201);
    const created = (await createRes.json()) as {
      id: string;
      attach_path: string;
    };

    // 3. GET now returns 200 with the same session.
    const live = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/session`,
      { headers: workspaceHeaders(workspaceSlug, token) },
    );
    expect(live.status).toBe(200);
    const body = (await live.json()) as {
      session_id: string;
      attach_path: string;
      created_at: string;
    };
    expect(body.session_id).toBe(created.id);
    expect(body.attach_path).toBe(created.attach_path);
    expect(typeof body.created_at).toBe("string");

    // 4. Delete the session — GET reverts to 204.
    await fetch(`${API_BASE}/api/cerebro/terminal/sessions/${created.id}`, {
      method: "DELETE",
      headers: workspaceHeaders(workspaceSlug, token),
    });
    const after = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/session`,
      { headers: workspaceHeaders(workspaceSlug, token) },
    );
    expect(after.status).toBe(204);
  });

  test("PUT presentation-mode rejects interactive for cloud runtimes", async () => {
    const api = new TestApiClient();
    await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
    const workspace = await api.ensureWorkspace(
      "E2E Terminal Workspace",
      DEFAULT_E2E_WORKSPACE,
    );
    const token = api.getToken();
    if (!token) throw new Error("login did not return a token");

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    let cloudRuntimeId: string | null = null;
    try {
      // Seed a runtime with the firtal-gateway cloud provider.
      const tag = `term-e2e-cloud-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const inserted = await client.query<{ id: string }>(
        `INSERT INTO agent_runtime
           (workspace_id, daemon_id, name, runtime_mode, provider, status, presentation_mode)
         VALUES ($1, $2, $3, 'cloud', 'firtal-gateway', 'online', 'headless')
         RETURNING id`,
        [workspace.id, tag, `Cloud E2E runtime ${tag}`],
      );
      cloudRuntimeId = inserted.rows[0]!.id;

      const setRes = await fetch(
        `${API_BASE}/api/cerebro/terminal/runtimes/${cloudRuntimeId}/presentation-mode`,
        {
          method: "PUT",
          headers: workspaceHeaders(workspace.slug, token),
          body: JSON.stringify({ presentation_mode: "interactive" }),
        },
      );
      expect(setRes.status).toBe(400);
      const body = (await setRes.json()) as { error?: string };
      expect(body.error).toBe("cloud_runtime_not_supported");
    } finally {
      if (cloudRuntimeId) {
        await client.query("DELETE FROM agent_runtime WHERE id = $1", [cloudRuntimeId]);
      }
      await client.end();
    }
  });

  test("GET/PUT presentation-mode round-trips against real DB", async () => {
    const { runtimeId: rid, workspaceSlug, token } = await seedInteractiveRuntime();
    runtimeId = rid;

    const getRes = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/presentation-mode`,
      { headers: workspaceHeaders(workspaceSlug, token) },
    );
    expect(getRes.status).toBe(200);
    const got = (await getRes.json()) as { presentation_mode: string };
    expect(got.presentation_mode).toBe("interactive");

    const putRes = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/presentation-mode`,
      {
        method: "PUT",
        headers: workspaceHeaders(workspaceSlug, token),
        body: JSON.stringify({ presentation_mode: "headless" }),
      },
    );
    expect(putRes.status).toBe(200);

    const get2 = await fetch(
      `${API_BASE}/api/cerebro/terminal/runtimes/${rid}/presentation-mode`,
      { headers: workspaceHeaders(workspaceSlug, token) },
    );
    const got2 = (await get2.json()) as { presentation_mode: string };
    expect(got2.presentation_mode).toBe("headless");
  });
});
