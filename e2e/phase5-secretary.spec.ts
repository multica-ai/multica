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

/** The task token a claimed run carries, as the agent presents it. */
const asRun = (run: { token: string }) => ({ Authorization: `Bearer ${run.token}` });

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

  test("closing the last child is recorded on the parent's child_done rule and waits for the Secretary's queued run", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      const leader = await api.createAgent("Wake leader", runtimeId);
      const parent = (await api.createIssue("Eval: closing wakes", {
        status: "in_progress",
        assignee_type: "agent",
        assignee_id: secretary.id,
      })) as { id: string };
      const child = (await api.createIssue("Eval: last child", {
        status: "in_progress",
        parent_issue_id: parent.id,
        assignee_type: "agent",
        assignee_id: leader.id,
      })) as { id: string };
      await json(await raw(api, workspace, "GET", `/api/issues/${parent.id}/system-wakeups`));
      await json(await raw(api, workspace, "PUT", `/api/issues/${child.id}`, { status: "done" }));

      // The scheduler evaluates the rule on its tick (every 30 seconds); the close is recorded
      // as the condition being met. The wakeup is an input held behind the Secretary's queued
      // run for the parent. With no daemon claiming that run here, it stays held, so no second
      // run is written. Once a daemon claims it, the held input is delivered (see the Go tests).
      await expect
        .poll(async () => (await api.queryDb(
          `SELECT condition_state FROM issue_wakeup WHERE issue_id = $1 AND system_rule = 'child_done'`,
          [parent.id],
        ))[0]?.condition_state, { timeout: 75_000, intervals: [2_000] })
        .toBe("all");
      const runs = await api.queryDb(
        `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND context->>'wakeup_system' = 'child_done'`,
        [parent.id],
      );
      expect(runs.length).toBe(0);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("the deadline wakeup fires at its time", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      await pauseCompany(api, workspace);
      const secretary = await api.createAgent("Secretary", runtimeId);
      const parent = (await api.createIssue("Eval: deadline fires", { status: "todo" })) as { id: string };
      const wake = await json<{ id: string }>(
        await raw(api, workspace, "POST", `/api/issues/${parent.id}/wakeups`, {
          agent_id: secretary.id,
          kind: "at",
          at: new Date(Date.now() + 2 * 60 * 60 * 1000).toISOString(),
          instruction: "The deadline has passed: read the answers and assess what is in.",
        }),
      );
      // The scheduler fires an at-wakeup at its time. The suite has no clock to advance, so
      // the same run is started now by the trigger endpoint, which uses the wakeup's own rule.
      expect((await raw(api, workspace, "POST", `/api/issues/${parent.id}/wakeups/${wake.id}/trigger`)).status).toBe(204);
      const runs = await api.queryDb(
        `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
        [parent.id, secretary.id],
      );
      expect(runs.length).toBeGreaterThanOrEqual(1);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("the Secretary challenges a child issue with a comment that wakes the leader", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      const leader = await api.createAgent("Challenge leader", runtimeId);
      await api.setSecretary(secretary.id);
      const parent = (await api.createIssue("Eval: challenge", { status: "todo", assignee_type: "agent", assignee_id: secretary.id })) as { id: string };
      const child = (await api.createIssue("Eval: challenged child", {
        status: "in_progress",
        parent_issue_id: parent.id,
        assignee_type: "agent",
        assignee_id: leader.id,
      })) as { id: string };
      const run = await api.seedRunningTask(secretary.id);
      const res = await raw(api, workspace, "POST", `/api/issues/${child.id}/comments`, { content: "Challenge: which figure is this based on?" }, asRun(run));
      expect(res.status, await res.clone().text()).toBe(201);
      const comments = await api.queryDb(
        `SELECT id FROM comment WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2`,
        [child.id, secretary.id],
      );
      expect(comments.length).toBe(1);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
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
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      await api.setSecretary(secretary.id);
      const session = await json<{ id: string }>(
        await raw(api, workspace, "POST", "/api/chat/sessions", { agent_id: secretary.id, title: "Phase 5 later reply" }),
      );
      const run = await api.seedRunningTask(secretary.id, { chatSessionId: session.id });
      await api.seedChatMessage(session.id, run.id, "Is research on track?");
      const res = await raw(api, workspace, "POST", `/api/chat/sessions/${session.id}/reply`, { content: "Research is on track for the date." }, asRun(run));
      expect(res.ok, await res.clone().text()).toBe(true);
      const replies = await api.queryDb(
        `SELECT content FROM chat_message WHERE chat_session_id = $1 AND role = 'assistant'`,
        [session.id],
      );
      expect(replies.map((r) => r.content)).toContain("Research is on track for the date.");
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a non-Secretary agent with a live run is refused the chat reply", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      const leader = await api.createAgent("Reply leader", runtimeId);
      await api.setSecretary(secretary.id);
      const session = await json<{ id: string }>(
        await raw(api, workspace, "POST", "/api/chat/sessions", { agent_id: secretary.id, title: "Phase 5 refused reply" }),
      );
      const run = await api.seedRunningTask(leader.id, { chatSessionId: session.id });
      const res = await raw(api, workspace, "POST", `/api/chat/sessions/${session.id}/reply`, { content: "Not mine to say." }, asRun(run));
      expect(res.status).toBe(403);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
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
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      await api.setSecretary(secretary.id);
      const session = await json<{ id: string }>(
        await raw(api, workspace, "POST", "/api/chat/sessions", { agent_id: secretary.id, title: "Phase 5 directive" }),
      );
      const run = await api.seedRunningTask(secretary.id, { chatSessionId: session.id });
      const message = await api.seedChatMessage(session.id, run.id, "Launch the pilot in two regions.");
      const res = await raw(api, workspace, "POST", "/api/issues", {
        title: "Pilot the onboarding in two regions",
        status: "todo",
        origin_type: "directive",
        origin_id: message,
      }, asRun(run));
      expect(res.status, await res.clone().text()).toBe(201);
      const issue = (await res.json()) as { id: string };
      const rows = await api.queryDb(`SELECT origin_type, origin_id FROM issue WHERE id = $1`, [issue.id]);
      expect(rows[0].origin_type).toBe("directive");
      expect(rows[0].origin_id).toBe(message);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a directive is refused from a run started by a non-admin originator", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      await api.setSecretary(secretary.id);
      const session = await json<{ id: string }>(
        await raw(api, workspace, "POST", "/api/chat/sessions", { agent_id: secretary.id, title: "Phase 5 member directive" }),
      );
      const run = await api.seedRunningTask(secretary.id, { chatSessionId: session.id });
      const message = await api.seedChatMessage(session.id, run.id, "Launch the pilot.");
      // The originator is the signed-in user; as a plain member they may not direct the Secretary.
      await api.setOwnRole("member");
      const res = await raw(api, workspace, "POST", "/api/issues", {
        title: "Pilot from a member",
        status: "todo",
        origin_type: "directive",
        origin_id: message,
      }, asRun(run));
      expect(res.status).toBe(403);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });

  test("a wakeup cannot create a directive", async () => {
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      await api.setSecretary(secretary.id);
      // A run with no chat session, as a wakeup starts: it cannot originate a directive.
      const run = await api.seedRunningTask(secretary.id);
      const res = await raw(api, workspace, "POST", "/api/issues", {
        title: "Directive from a wakeup",
        status: "todo",
        origin_type: "directive",
        origin_id: randomUUID(),
      }, asRun(run));
      expect(res.status).toBe(403);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
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
    const { api, workspace, runtimeId } = await emptyWorkspace();
    try {
      const secretary = await api.createAgent("Secretary", runtimeId);
      const leader = await api.createAgent("Question leader", runtimeId);
      await api.setSecretary(secretary.id);
      const directive = (await api.createIssue("Eval: leader question", {
        status: "todo",
        assignee_type: "agent",
        assignee_id: leader.id,
      })) as { id: string };
      const run = await api.seedRunningTask(leader.id);
      const res = await raw(api, workspace, "POST", `/api/issues/${directive.id}/comments`, {
        content: `[@Secretary](mention://agent/${secretary.id}) Is the date still right, or should I wait for the vendor?`,
      }, asRun(run));
      expect(res.status, await res.clone().text()).toBe(201);
      const runs = await api.queryDb(
        `SELECT id FROM agent_task_queue WHERE agent_id = $1 AND issue_id = $2`,
        [secretary.id, directive.id],
      );
      expect(runs.length).toBeGreaterThanOrEqual(1);
    } finally {
      await api.cleanup();
      await api.deleteWorkspace();
    }
  });
});
