// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useIssueDraftStore } from "./draft-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

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
});
