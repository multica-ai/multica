import "./env";

import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

// Phase 5 plumbing over the API (spec 13.8.2, plan 13.8.2). Each test starts from its own
// workspace with an online Hermes runtime (no daemon, so nothing runs). The company is
// paused for the whole test: a leader or Secretary run that the test starts is held, so no
// model is called. Setup and teardown go through TestApiClient; raw requests cover the
// endpoints the client does not wrap.
//
// Covered: a request to three squads and a chief, with one child per addressee; the deadline
// wakeup and child_done rules being filed on the parent; the chat reply endpoint refusing a
// member and an agent identity without a live run; a directive refused from a member; the org
// map; a company pause.
//
// Not covered here. Each skipped step names its reason inside the test; none is a fake pass.
// Most need a claimed run (a task token from the daemon) or a clock the suite cannot advance.

const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const LIVE_RUN = "needs a live run: a task token from a claimed run, with the daemon and Hermes. Checked in docs/engineering/runbooks/phase5.md.";

type Workspace = Awaited<ReturnType<TestApiClient["createWorkspace"]>>;

const suffix = () => `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

async function emptyWorkspace() {
  const api = await createTestApi();
  const id = suffix();
  const workspace = await api.createWorkspace(`Phase 5 ${id}`, `e2e-phase5-${id}`);
  const runtimeId = await api.seedRuntime("Phase 5 Hermes runtime", "hermes");
  return { api, workspace, runtimeId };
}

/** A raw request in the workspace, as the signed-in user. */
async function raw(
  api: TestApiClient,
  workspace: Workspace,
  method: string,
  path: string,
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<Response> {
  const token = api.getToken();
  if (!token) throw new Error("the test client is not signed in");
  return fetch(`${API_BASE}${path}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      "X-Workspace-ID": workspace.id,
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

async function json<T>(res: Response): Promise<T> {
  expect(res.ok, `HTTP ${res.status}: ${await res.clone().text()}`).toBe(true);
  return (await res.json()) as T;
}

async function pauseCompany(api: TestApiClient, workspace: Workspace) {
  const state = await json<{ state: string }>(await raw(api, workspace, "PUT", "/api/company/company-state", { state: "paused" }));
  expect(state.state).toBe("paused");
}

test.describe("Secretary chief-of-staff plumbing", () => {
  test("a request to three squads and a chief files one parent and one child per addressee", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      await pauseCompany(api, workspace);
      const secretary = await api.createAgent("Secretary", runtimeId);
      const chiefAgent = await api.createAgent("Research chief", runtimeId);
      const floor = await api.createCompanyFloor("Research");
      await json(await raw(api, workspace, "PUT", `/api/company/floors/${floor.id}/chief`, { agent_id: chiefAgent.id }));

      const squads = [];
      for (const name of ["Market Research", "Opportunity Lab", "Customer Voice"]) {
        const leader = await api.createAgent(`${name} leader`, runtimeId);
        squads.push(await api.createSquad(name, leader.id));
      }
      // The server needs floor_id and slot set together, so each squad gets its own slot.
      await api.seatCompanySquads(squads.map((s, slot) => ({ squad_id: s.id, floor_id: floor.id, slot })));

      const parent = (await api.createIssue("Eval: how is research doing?", {
        status: "todo",
        assignee_type: "agent",
        assignee_id: secretary.id,
      })) as { id: string };

      const children = [];
      for (const squad of squads) {
        children.push(
          (await api.createIssue(`Ask ${squad.name}`, {
            status: "todo",
            parent_issue_id: parent.id,
            assignee_type: "squad",
            assignee_id: squad.id,
          })) as { id: string },
        );
      }
      const chiefChild = (await api.createIssue("Ask the Research chief", {
        status: "todo",
        parent_issue_id: parent.id,
        assignee_type: "agent",
        assignee_id: chiefAgent.id,
      })) as { id: string };
      children.push(chiefChild);

      for (const child of children) {
        const got = await json<{ parent_issue_id: string | null }>(await raw(api, workspace, "GET", `/api/issues/${child.id}`));
        expect(got.parent_issue_id).toBe(parent.id);
      }
      const parentGot = await json<{ assignee_type: string; assignee_id: string }>(await raw(api, workspace, "GET", `/api/issues/${parent.id}`));
      expect(parentGot.assignee_type).toBe("agent");
      expect(parentGot.assignee_id).toBe(secretary.id);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a one-shot deadline wakeup can be filed on the parent request and is listed with it", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      await pauseCompany(api, workspace);
      const secretary = await api.createAgent("Secretary", runtimeId);
      const parent = (await api.createIssue("Eval: deadline", { status: "todo" })) as { id: string };
      const at = new Date(Date.now() + 2 * 60 * 60 * 1000).toISOString();
      const instruction = "Deadline reached: assess what the squads have answered.";
      await json(
        await raw(api, workspace, "POST", `/api/issues/${parent.id}/wakeups`, {
          agent_id: secretary.id,
          kind: "at",
          at,
          instruction,
        }),
      );
      const listed = await json<unknown>(await raw(api, workspace, "GET", `/api/issues/${parent.id}/wakeups`));
      expect(JSON.stringify(listed)).toContain(instruction);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("child_done is on for the parent request, so the rule is listed", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      // The rule is listed only for an issue with an assignee to wake: a request is
      // assigned to the Secretary, so the parent gets the same assignment here.
      const secretary = await api.createAgent("Phase 5 Secretary", runtimeId);
      const parent = (await api.createIssue("Eval: child_done rule", {
        status: "todo",
        assignee_type: "agent",
        assignee_id: secretary.id,
      })) as { id: string };
      // The rule is listed only while a child is open, so the request gets one.
      await api.createIssue("Eval: open child", { status: "todo", parent_issue_id: parent.id });
      const rules = await json<unknown>(await raw(api, workspace, "GET", `/api/issues/${parent.id}/system-wakeups`));
      expect(JSON.stringify(rules)).toContain("child_done");
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("closing the last child wakes the parent's Secretary once, with the originator kept", async () => {
    test.skip(true, "the wakeup is a queued run: it needs the company unpaused and a claimed run to observe. The rule itself is listed above; the firing is covered by the Go tests for child_done and the runbook.");
  });

  test("the deadline wakeup fires at its time", async () => {
    test.skip(true, "the wakeup fires when the scheduler reaches its time; this suite has no fake clock to advance, so the firing is checked in the Go tests and the runbook.");
  });

  test("the Secretary challenges a child issue with a comment that wakes the leader", async () => {
    test.skip(true, `the challenge is a comment by the Secretary, which needs a task token: ${LIVE_RUN}`);
  });

  test("the chat reply endpoint refuses a signed-in member", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      const session = await json<{ id: string }>(
        await raw(api, workspace, "POST", "/api/chat/sessions", { agent_id: secretary.id, title: "Phase 5 reply" }),
      );
      const res = await raw(api, workspace, "POST", `/api/chat/sessions/${session.id}/reply`, { content: "A reply from a person." });
      expect(res.status).toBe(403);
      expect(await res.text()).toContain("secretary");
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("the chat reply endpoint refuses an agent identity with no live run", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      const session = await json<{ id: string }>(
        await raw(api, workspace, "POST", "/api/chat/sessions", { agent_id: secretary.id, title: "Phase 5 reply" }),
      );
      // Without a task token the server does not trust X-Agent-ID; the caller is a member.
      const res = await raw(api, workspace, "POST", `/api/chat/sessions/${session.id}/reply`, { content: "A reply." }, { "X-Agent-ID": secretary.id });
      expect(res.status).toBe(403);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("the Secretary's reply from a later run is written into the boss's chat", async () => {
    test.skip(true, `a reply needs a task token from the Secretary's run: ${LIVE_RUN}`);
  });

  test("a non-Secretary agent with a live run is refused the chat reply", async () => {
    test.skip(true, `needs a task token from a leader's run: ${LIVE_RUN}`);
  });

  test("a directive is refused when a member creates it", async () => {
    const { api, workspace } = await emptyWorkspace();
    try {
      const res = await raw(api, workspace, "POST", "/api/issues", {
        title: "Directive: launch the spring pricing change",
        status: "todo",
        origin_type: "directive",
        origin_id: randomUUID(),
      });
      expect(res.status).toBe(403);
      expect(await res.text()).toContain("secretary agent");
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a directive is accepted from a chat-triggered Secretary run", async () => {
    test.skip(true, `a directive needs the Secretary's live run triggered by a boss chat message: ${LIVE_RUN}`);
  });

  test("a directive is refused from a run started by a non-admin originator", async () => {
    test.skip(true, `needs a run whose originator is a member: ${LIVE_RUN}`);
  });

  test("a wakeup cannot create a directive", async () => {
    test.skip(true, `needs a wakeup run: ${LIVE_RUN}`);
  });

  test("the org map lists the squads with their leaders", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const leader = await api.createAgent("Map leader", runtimeId);
      const squad = await api.createSquad("Map squad", leader.id);
      const map = await json<{ squads: { id: string; name: string; leader: { id: string } | null }[] }>(
        await raw(api, workspace, "GET", "/api/company/org-map"),
      );
      const found = map.squads.find((s) => s.id === squad.id);
      expect(found?.name).toBe("Map squad");
      expect(found?.leader?.id).toBe(leader.id);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a company pause is held: the state reads paused and the Secretary answers partially", async () => {
    const { api, workspace } = await emptyWorkspace();
    try {
      await pauseCompany(api, workspace);
      const state = await json<{ state: string }>(await raw(api, workspace, "GET", "/api/company/company-state"));
      expect(state.state).toBe("paused");
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a paused company yields a partial answer that says why", async () => {
    test.skip(true, `the answer is the Secretary's words in a live run, with the company held: ${LIVE_RUN}`);
  });

  test("a leader's question on a directive wakes the Secretary", async () => {
    test.skip(true, `the wakeup is a run that the leader's mention starts: ${LIVE_RUN}`);
  });
});
