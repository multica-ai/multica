// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useIssueDraftStore } from "./draft-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import { resetAllRegisteredDrafts } from "../../drafts/cleanup-registry";
import { clearWorkspaceStorage } from "../../platform/storage-cleanup";
import { defaultStorage } from "../../platform/storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

const RESET_STATE = {
  draft: {
    shared: {
      projectId: undefined,
      priority: "none" as const,
      dueDate: null,
      attachments: [],
    },
    manual: {
      title: "",
      description: "",
      status: "todo" as const,
      startDate: null,
      assigneeType: undefined,
      assigneeId: undefined,
      labelIds: [],
      propertyValues: {},
    },
    agent: {
      prompt: "",
      actorType: undefined,
      actorId: undefined,
    },
    activeMode: "manual" as const,
  },
  lastAssigneeType: undefined,
  lastAssigneeId: undefined,
  batchContinuationBaseline: undefined,
};

describe("issue draft store — last assignee", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  it("clearDraft prefills the next manual draft with the remembered assignee", () => {
    const { setManual, setLastAssignee, clearDraft } =
      useIssueDraftStore.getState();

    setManual({ title: "first", assigneeType: "member", assigneeId: "alice" });
    setLastAssignee("member", "alice");
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.manual.title).toBe("");
    expect(draft.manual.assigneeType).toBe("member");
    expect(draft.manual.assigneeId).toBe("alice");
  });

  it("clearDraft resets status for a fresh create", () => {
    const { setManual, clearDraft } = useIssueDraftStore.getState();
    setManual({ status: "done" });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.manual.status).toBe("todo");
  });

  it("persists todo for an untouched batch continuation but keeps an explicitly edited draft", async () => {
    setCurrentWorkspace("batch", "ws_batch");
    await flush();
    await flush();

    const storedStatus = () =>
      JSON.parse(localStorage.getItem("multica_issue_draft:batch") ?? "{}").state.draft.manual.status;

    const store = useIssueDraftStore.getState();
    // "Create another" resets the form but carries the submitted status and
    // assignee into the next issue, so the reset itself is not user input.
    store.setManual({ status: "in_progress", assigneeType: "member", assigneeId: "alice" });
    store.beginBatchContinuation();
    expect(storedStatus()).toBe("todo");

    // A field the user changed IS input, even before a title is typed.
    useIssueDraftStore.getState().setManual({ status: "done" });
    expect(storedStatus()).toBe("done");

    // As is a started next issue.
    useIssueDraftStore.getState().setManual({ title: "Unfinished next issue" });
    expect(storedStatus()).toBe("done");
    setCurrentWorkspace(null, null);
  });

  it("persists an explicitly selected status on a fresh create before any title is typed", async () => {
    setCurrentWorkspace("status-only", "ws_status");
    await flush();
    await flush();

    useIssueDraftStore.getState().setManual({ status: "in_progress" });

    const stored = JSON.parse(localStorage.getItem("multica_issue_draft:status-only") ?? "{}");
    expect(stored.state.draft.manual.status).toBe("in_progress");
    setCurrentWorkspace(null, null);
  });

  it("rehydrates an explicitly selected status that never got a title", async () => {
    setCurrentWorkspace("status-rehydrate", "ws_status");
    await flush();
    await flush();

    useIssueDraftStore.getState().setManual({ status: "in_progress" });
    expect(
      JSON.parse(localStorage.getItem("multica_issue_draft:status-rehydrate") ?? "{}")
        .state.draft.manual.status,
    ).toBe("in_progress");

    // Leave and come back to the workspace: the persisted selection returns.
    setCurrentWorkspace(null, null);
    await flush();
    setCurrentWorkspace("status-rehydrate", "ws_status");
    await flush();
    await flush();

    expect(useIssueDraftStore.getState().draft.manual.status).toBe("in_progress");
    setCurrentWorkspace(null, null);
  });

  it("clearDraft yields an empty assignee when none has ever been remembered", () => {
    const { setManual, clearDraft } = useIssueDraftStore.getState();

    setManual({ title: "first" });
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.manual.assigneeType).toBeUndefined();
    expect(draft.manual.assigneeId).toBeUndefined();
  });

  it("clearDraft removes persisted shared attachments", () => {
    const { setShared, clearDraft } = useIssueDraftStore.getState();

    setShared({
      attachments: [
        {
          clientUploadId: "11111111-2222-3333-4444-555555555555",
          status: "uploaded",
          filename: "shot.png",
          size: 123,
          contentType: "image/png",
          attachment: {
            id: "11111111-2222-3333-4444-555555555555",
            workspace_id: "ws-1",
            issue_id: null,
            comment_id: null,
            chat_session_id: null,
            chat_message_id: null,
            uploader_type: "member",
            uploader_id: "alice",
            filename: "shot.png",
            url: "https://cdn.example.test/shot.png",
            download_url: "https://cdn.example.test/shot.png",
            markdown_url: "https://app.example.test/api/attachments/11111111-2222-3333-4444-555555555555/download",
            content_type: "image/png",
            size_bytes: 123,
            created_at: "2026-06-12T00:00:00Z",
          },
        },
      ],
    });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.shared.attachments).toEqual([]);
  });

  it("clearDraft removes persisted custom property values", () => {
    const { setManual, clearDraft } = useIssueDraftStore.getState();

    setManual({ propertyValues: { "property-1": "option-1" } });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.manual.propertyValues).toEqual({});
  });

  it("isolates a source-context create draft and restores the ordinary draft", () => {
    const store = useIssueDraftStore.getState();
    store.setManual({ title: "ordinary draft" });
    const ordinary = useIssueDraftStore.getState().draft;

    store.beginIsolatedDraft();
    expect(useIssueDraftStore.getState().draft.manual.title).toBe("");
    useIssueDraftStore.getState().setManual({ title: "source-only draft" });
    expect(useIssueDraftStore.getState().draft.manual.title).toBe("source-only draft");

    useIssueDraftStore.getState().endIsolatedDraft();
    expect(useIssueDraftStore.getState().draft).toEqual(ordinary);
    expect(useIssueDraftStore.getState().isolatedDraftBackup).toBeUndefined();
  });

  it("clearDraft removes the persisted project selection", () => {
    const { setShared, clearDraft } = useIssueDraftStore.getState();

    setShared({ projectId: "project-1" });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.shared.projectId).toBeUndefined();
  });

  it("clearDraft removes the persisted agent prompt", () => {
    const { setAgent, clearDraft } = useIssueDraftStore.getState();

    setAgent({ prompt: "Investigate the regression" });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.agent.prompt).toBe("");
  });

  it("setLastAssignee(undefined) lets the user opt back out of a default", () => {
    const { setLastAssignee, clearDraft } = useIssueDraftStore.getState();

    setLastAssignee("member", "alice");
    clearDraft();
    expect(useIssueDraftStore.getState().draft.manual.assigneeId).toBe("alice");

    setLastAssignee(undefined, undefined);
    clearDraft();
    expect(useIssueDraftStore.getState().draft.manual.assigneeId).toBeUndefined();
    expect(useIssueDraftStore.getState().draft.manual.assigneeType).toBeUndefined();
  });
});

describe("issue draft store — keep-open batch continuation", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  // Stands in for `resetForNextIssue` in the create dialog: the batch reset
  // writes the carried values, then marks the result as the baseline.
  function beginContinuation() {
    const store = useIssueDraftStore.getState();
    store.setManual({ status: "in_progress", assigneeType: "member", assigneeId: "alice" });
    store.beginBatchContinuation();
  }

  it("does not treat an untouched continuation's inherited fields as a draft", () => {
    beginContinuation();

    expect(useIssueDraftStore.getState().hasDraft()).toBe(false);
  });

  it.each([
    ["priority", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setShared({ priority: "high" })],
    ["project", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setShared({ projectId: "project-1" })],
    ["due date", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setShared({ dueDate: "2026-10-01" })],
    ["start date", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setManual({ startDate: "2026-10-01" })],
    ["labels", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setManual({ labelIds: ["label-1"] })],
    ["status", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setManual({ status: "done" })],
    ["assignee", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setManual({ assigneeId: "bob" })],
    ["custom properties", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setManual({ propertyValues: { "property-1": "option-1" } })],
    ["title", (s: ReturnType<typeof useIssueDraftStore.getState>) => s.setManual({ title: "Next issue" })],
  ])("keeps a %s-only edit recoverable after a keep-open reset", (_field, edit) => {
    beginContinuation();
    expect(useIssueDraftStore.getState().hasDraft()).toBe(false);

    edit(useIssueDraftStore.getState());

    expect(useIssueDraftStore.getState().hasDraft()).toBe(true);
  });

  it("does not treat a mode switch on an untouched continuation as an edit", () => {
    beginContinuation();

    // `switchToAgent` commits the manual form's shared fields, flips the active
    // mode and (on a blank form) leaves the agent slot empty. None of that is
    // user intent, so the continuation is still untouched.
    const store = useIssueDraftStore.getState();
    store.setShared({ projectId: undefined, priority: "none", dueDate: null });
    store.setActiveMode("agent");

    expect(useIssueDraftStore.getState().hasDraft()).toBe(false);
  });

  it("persists todo for a continuation that was only switched to agent mode", async () => {
    setCurrentWorkspace("mode-switch", "ws_mode");
    await flush();
    await flush();

    const storedStatus = () =>
      JSON.parse(localStorage.getItem("multica_issue_draft:mode-switch") ?? "{}").state.draft.manual.status;

    beginContinuation();
    expect(storedStatus()).toBe("todo");

    const store = useIssueDraftStore.getState();
    store.setShared({ projectId: undefined, priority: "none", dueDate: null });
    store.setActiveMode("agent");

    // A mode switch is not an edit: the batch's status must not survive into
    // the next fresh create through storage either.
    expect(storedStatus()).toBe("todo");
    setCurrentWorkspace(null, null);
  });

  it("does not treat a mode switch's agent assist-init as an edit", () => {
    const store = useIssueDraftStore.getState();
    // A batch that inherited an agent assignee: `switchToAgent` hands it to the
    // agent slot so the next issue can be filed by the same agent.
    store.setManual({ status: "in_progress", assigneeType: "agent", assigneeId: "agent-1" });
    useIssueDraftStore.getState().beginBatchContinuation();

    const switching = useIssueDraftStore.getState();
    switching.setShared({ projectId: undefined, priority: "none", dueDate: null });
    switching.setAgent({ actorType: "agent", actorId: "agent-1" });
    switching.setActiveMode("agent");

    // The agent slot belongs to the other form, so filling it is not an edit of
    // the continuation the manual form left behind.
    expect(useIssueDraftStore.getState().hasDraft()).toBe(false);
  });

  it("keeps an edited draft and its status recoverable across a mode switch", async () => {
    setCurrentWorkspace("switch-edit", "ws_switch");
    await flush();
    await flush();

    const storedDraft = () =>
      JSON.parse(localStorage.getItem("multica_issue_draft:switch-edit") ?? "{}").state.draft ?? {};

    beginContinuation();
    // A real selection for the next issue, re-committed by the switch along
    // with the mode flip.
    const store = useIssueDraftStore.getState();
    store.setShared({ priority: "high" });
    store.setShared({ projectId: undefined, priority: "high", dueDate: null });
    store.setActiveMode("agent");

    expect(useIssueDraftStore.getState().hasDraft()).toBe(true);
    expect(storedDraft().shared?.priority).toBe("high");
    expect(storedDraft().manual?.status).toBe("in_progress");
    setCurrentWorkspace(null, null);
  });

  it("ends an untouched continuation by normalizing only its inherited status", () => {
    beginContinuation();
    useIssueDraftStore.getState().endBatchContinuation();

    const state = useIssueDraftStore.getState();
    expect(state.draft.manual.status).toBe("todo");
    // Closing is not a fresh create: the rest of the reset — the carried
    // assignee, the shared slot — is left exactly as it was.
    expect(state.draft.manual.assigneeId).toBe("alice");
    expect(state.draft.shared.priority).toBe("none");
    expect(state.batchContinuationBaseline).toBeUndefined();
  });

  it("keeps an edited continuation as it is, inherited status included", () => {
    beginContinuation();
    const store = useIssueDraftStore.getState();
    store.setShared({ priority: "high" });
    store.setAgent({ prompt: "audit the flaky test" });

    useIssueDraftStore.getState().endBatchContinuation();

    const state = useIssueDraftStore.getState();
    expect(state.draft.shared.priority).toBe("high");
    // Agent-side content is a real draft, not continuation bookkeeping.
    expect(state.draft.agent.prompt).toBe("audit the flaky test");
    // An edited continuation is the user's unfinished next issue, so the
    // status the batch inherited stays with it.
    expect(state.draft.manual.status).toBe("in_progress");
    expect(state.batchContinuationBaseline).toBeUndefined();
  });

  it("keeps a status the user picked in the continuation", () => {
    beginContinuation();
    useIssueDraftStore.getState().setManual({ status: "done" });

    useIssueDraftStore.getState().endBatchContinuation();

    expect(useIssueDraftStore.getState().draft.manual.status).toBe("done");
  });

  it("normalizes a mode-only continuation's status and keeps its seeded agent prompt", () => {
    const store = useIssueDraftStore.getState();
    store.setManual({ status: "in_progress", assigneeType: "agent", assigneeId: "agent-1" });
    useIssueDraftStore.getState().beginBatchContinuation();

    // `switchToAgent` commits the unchanged shared fields, seeds the agent slot
    // from the manual form and flips the mode — none of it a manual edit.
    const switching = useIssueDraftStore.getState();
    switching.setShared({ projectId: undefined, priority: "none", dueDate: null });
    switching.setAgent({ prompt: "audit the flaky test", actorType: "agent", actorId: "agent-1" });
    switching.setActiveMode("agent");

    useIssueDraftStore.getState().endBatchContinuation();

    const state = useIssueDraftStore.getState();
    // Only the inherited status is dropped; the agent side is left intact.
    expect(state.draft.manual.status).toBe("todo");
    expect(state.draft.agent.prompt).toBe("audit the flaky test");
    expect(state.draft.agent.actorId).toBe("agent-1");
    expect(state.draft.activeMode).toBe("agent");
    expect(state.batchContinuationBaseline).toBeUndefined();
  });

  it("leaves the restored ordinary draft alone when the snapshot is already gone", () => {
    // The source-context wrapper restores the ordinary draft and drops the
    // snapshot before the manual panel's close cleanup runs.
    useIssueDraftStore.getState().setShared({ priority: "high" });
    useIssueDraftStore.getState().setManual({ status: "backlog" });
    const ordinary = useIssueDraftStore.getState().draft;

    useIssueDraftStore.getState().endBatchContinuation();

    expect(useIssueDraftStore.getState().draft).toBe(ordinary);
  });

  it("stops tracking a continuation once the draft is cleared", () => {
    beginContinuation();
    useIssueDraftStore.getState().clearDraft();
    expect(useIssueDraftStore.getState().batchContinuationBaseline).toBeUndefined();

    // The fresh draft is judged on content alone again — and its status is the
    // reset todo, not whatever the batch had submitted.
    useIssueDraftStore.getState().setManual({ status: "in_progress" });
    expect(useIssueDraftStore.getState().draft.manual.status).toBe("in_progress");
    expect(useIssueDraftStore.getState().hasDraft()).toBe(false);
  });

  it("drops the continuation snapshot when an isolated draft session starts", () => {
    beginContinuation();
    const isolated = useIssueDraftStore.getState();
    isolated.beginIsolatedDraft();
    expect(useIssueDraftStore.getState().batchContinuationBaseline).toBeUndefined();

    useIssueDraftStore.getState().endIsolatedDraft();
    expect(useIssueDraftStore.getState().batchContinuationBaseline).toBeUndefined();
  });
});

describe("issue draft store — mode switch preserves both sides", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  it("keeps the manual slot untouched when the agent slot is filled and vice versa", () => {
    const { setManual, setAgent, setShared } = useIssueDraftStore.getState();

    setManual({ title: "Manual title", description: "Manual body" });
    setShared({ projectId: "project-1", priority: "high" });
    // Switching to agent seeds the agent slot but must not clear manual.
    setAgent({ prompt: "Agent prompt", actorType: "agent", actorId: "agent-1" });

    const { draft } = useIssueDraftStore.getState();
    expect(draft.manual.title).toBe("Manual title");
    expect(draft.manual.description).toBe("Manual body");
    expect(draft.agent.prompt).toBe("Agent prompt");
    expect(draft.agent.actorId).toBe("agent-1");
    // Shared fields are visible to both sides.
    expect(draft.shared.projectId).toBe("project-1");
    expect(draft.shared.priority).toBe("high");
  });
});

describe("issue draft store — legacy rehydrate", () => {
  beforeEach(() => {
    localStorage.clear();
    setCurrentWorkspace(null, null);
  });

  afterEach(() => {
    setCurrentWorkspace(null, null);
  });

  it("drops obsolete status and stage preferences from persisted state", async () => {
    localStorage.setItem(
      "multica_issue_draft:legacy-pref",
      JSON.stringify({
        state: {
          draft: { manual: { status: "todo" } },
          lastStatus: "done",
          lastStage: 3,
          lastStageParentIssueId: "parent-a",
        },
        version: 0,
      }),
    );

    setCurrentWorkspace("legacy-pref", "ws_pref");
    await flush();
    await flush();

    const state = useIssueDraftStore.getState();
    expect("lastStatus" in state).toBe(false);
    expect("lastStage" in state).toBe(false);
    expect("lastStageParentIssueId" in state).toBe(false);
    state.setManual({ title: "new draft" });
    const stored = JSON.parse(localStorage.getItem("multica_issue_draft:legacy-pref") ?? "{}");
    expect(stored.state).not.toHaveProperty("lastStatus");
    expect(stored.state).not.toHaveProperty("lastStage");
    expect(stored.state).not.toHaveProperty("lastStageParentIssueId");
  });

  it("migrates a pre-MUL-5181 flat draft into the shared/manual slots", async () => {
    localStorage.setItem(
      "multica_issue_draft:acme",
      JSON.stringify({
        state: {
          draft: {
            title: "legacy",
            description: "body",
            status: "todo",
            priority: "high",
            projectId: "project-1",
            startDate: null,
            dueDate: "2026-08-01",
            labelIds: ["label-1"],
            propertyValues: { "property-1": "option-1" },
            // no `attachments` — written by a build that predates the field
          },
        },
        version: 0,
      }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const { draft } = useIssueDraftStore.getState();
    // Manual-only fields land in the manual slot.
    expect(draft.manual.title).toBe("legacy");
    expect(draft.manual.description).toBe("body");
    expect(draft.manual.status).toBe("todo");
    expect(draft.manual.labelIds).toEqual(["label-1"]);
    expect(draft.manual.propertyValues).toEqual({ "property-1": "option-1" });
    // Shared fields land in the shared slot, with attachments backfilled.
    expect(draft.shared.projectId).toBe("project-1");
    expect(draft.shared.priority).toBe("high");
    expect(draft.shared.dueDate).toBe("2026-08-01");
    expect(draft.shared.attachments).toEqual([]);
    // A legacy draft had no agent prompt (it lived in a separate store).
    expect(draft.agent.prompt).toBe("");
    expect(draft.activeMode).toBe("manual");
  });

  it("backfills missing sub-fields on an already-nested persisted draft", async () => {
    localStorage.setItem(
      "multica_issue_draft:beta",
      JSON.stringify({
        state: {
          draft: {
            // A build that shipped the nested shape but predated a later field.
            shared: { projectId: "project-2", priority: "urgent" },
            manual: { title: "kept" },
            agent: { prompt: "keep me" },
            activeMode: "agent",
          },
        },
        version: 0,
      }),
    );

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.shared.projectId).toBe("project-2");
    expect(draft.shared.priority).toBe("urgent");
    expect(draft.shared.dueDate).toBeNull();
    expect(draft.shared.attachments).toEqual([]);
    expect(draft.manual.title).toBe("kept");
    expect(draft.manual.status).toBe("todo");
    expect(draft.agent.prompt).toBe("keep me");
    expect(draft.activeMode).toBe("agent");
  });

  it("normalizes pre-L2 shared attachments and drops stale uploading placeholders", async () => {
    localStorage.setItem(
      "multica_issue_draft:gamma",
      JSON.stringify({
        state: {
          draft: {
            shared: {
              attachments: [
                // Pre-L2 build persisted a bare Attachment row.
                {
                  id: "att-1",
                  filename: "old.png",
                  url: "https://cdn.example.test/old.png",
                  size_bytes: 7,
                  content_type: "image/png",
                },
                // An upload that was still in flight when the app closed.
                {
                  clientUploadId: "c-flight",
                  status: "uploading",
                  filename: "mid.png",
                  size: 9,
                },
              ],
            },
            manual: {},
            agent: {},
            activeMode: "manual",
          },
        },
        version: 0,
      }),
    );

    setCurrentWorkspace("gamma", "ws_c");
    await flush();
    await flush();

    const { attachments } = useIssueDraftStore.getState().draft.shared;
    // The legacy row is adopted; the in-flight one is dropped — it can neither
    // resume nor be shown, so keeping it would only bloat the draft.
    expect(attachments.map((u) => u.status)).toEqual(["uploaded"]);
    expect(attachments[0]?.filename).toBe("old.png");
  });
});

describe("issue draft store — hasDraft upload semantics", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  const placeholder = (status: "uploading" | "uploaded" | "failed" | "interrupted") =>
    ({
      clientUploadId: `c-${status}`,
      status,
      filename: "f.png",
      size: 1,
      ...(status === "uploaded"
        ? {
            attachment: {
              id: "att-1",
              filename: "f.png",
              url: "https://cdn.example.test/f.png",
            },
          }
        : {}),
    }) as never;

  it("counts uploaded and uploading entries as recoverable draft intent", () => {
    const { setShared, hasDraft } = useIssueDraftStore.getState();
    setShared({ attachments: [placeholder("uploading")] });
    expect(hasDraft()).toBe(true);
    setShared({ attachments: [placeholder("uploaded")] });
    expect(hasDraft()).toBe(true);
  });

  it("ignores failed/interrupted remnants so they cannot pin the sidebar dot", () => {
    const { setShared, hasDraft } = useIssueDraftStore.getState();
    setShared({ attachments: [placeholder("failed"), placeholder("interrupted")] });
    expect(hasDraft()).toBe(false);
  });
});

describe("issue draft store — logout cleanup", () => {
  beforeEach(() => {
    localStorage.clear();
    setCurrentWorkspace(null, null);
    useIssueDraftStore.setState(RESET_STATE);
  });

  afterEach(() => {
    setCurrentWorkspace(null, null);
  });

  it("registered reset wipes the last-assignee preference, not just the draft", () => {
    const { setManual, setLastAssignee } = useIssueDraftStore.getState();
    setManual({ title: "wip", assigneeType: "member", assigneeId: "alice" });
    setLastAssignee("member", "alice");

    resetAllRegisteredDrafts();

    const state = useIssueDraftStore.getState();
    expect(state.lastAssigneeType).toBeUndefined();
    expect(state.lastAssigneeId).toBeUndefined();
    // clearDraft() would have re-seeded the manual slot from lastAssignee —
    // the logout reset must not hand the next login a previous user's pick.
    expect(state.draft.manual.assigneeType).toBeUndefined();
    expect(state.draft.manual.assigneeId).toBeUndefined();
  });

  it("logout sequence (reset in-memory, then clear storage) leaves no persisted key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const { setManual, setLastAssignee } = useIssueDraftStore.getState();
    setManual({ title: "secret wip" });
    setLastAssignee("member", "alice");
    expect(localStorage.getItem("multica_issue_draft:acme")).not.toBeNull();

    // use-logout's order: reset first (each reset is a setState, and persist
    // writes the new state back to storage under the still-active slug), THEN
    // remove the keys. The reverse order resurrects the key with the previous
    // user's lastAssignee inside.
    resetAllRegisteredDrafts();
    clearWorkspaceStorage(defaultStorage, "acme");

    expect(localStorage.getItem("multica_issue_draft:acme")).toBeNull();
  });
});
