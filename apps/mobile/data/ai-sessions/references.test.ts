// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import { AgentSchema, EMPTY_ISSUE_FALLBACK } from "@/data/schemas";
import { agentReferenceUnavailable, prepareAIMessage, readReferenceMessage, referenceMessage, toggleReference, type AIReference, type ReferenceResolver } from "./references";

const session: AIReference = { kind: "session", id: "session-1", name: "Earlier discussion" };
const issue: AIReference = { kind: "issue", id: "issue-1", name: "CHOV-128" };
const agent: AIReference = { kind: "agent", id: "agent-1", name: "Reviewer" };
const reviewer = () => AgentSchema.parse({ id: "agent-1", workspace_id: "workspace-1", name: "Reviewer", runtime_id: "runtime-1", owner_id: "user-1" });
function resolver(): ReferenceResolver {
  return { workspaceId: "workspace-1", userId: "user-1",
    session: vi.fn(async () => localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 1, conversation: { conversation_id: "session-1", title: "Earlier discussion", summary_seq: 1, provider: "codex", status: "idle", pinned: false, archived: false, updated_at: "now" }, turns: [{ id: "turn-1", turn_index: 0, status: "completed", started_at: "now", messages: [{ id: "m1", role: "user", content: "Earlier question", created_at: "now" }], events: [{ seq: 1, type: "item.completed", final: true, item_id: "item-1", payload: { kind: "assistant_text", text: "Earlier answer" }, created_at: "now" }] }] })),
    issue: vi.fn(async () => ({ ...EMPTY_ISSUE_FALLBACK, id: "issue-1", workspace_id: "workspace-1", title: "Add mentions", identifier: "CHOV-128", description: "Task context" })),
    agentCatalog: vi.fn(async () => ({ agents: [reviewer()], role: "member" as const })),
  };
}

describe("AI reference selection and display", () => {
  it("deduplicates by kind and ID, permits deselection at five and preserves labels", () => {
    const five = [session, issue, agent, { ...session, id: "s2" }, { ...session, id: "s3" }];
    expect(toggleReference(five, { ...session, id: "s4" })).toBe(five);
    expect(toggleReference(five, session)).toHaveLength(4);
    expect(toggleReference([session], { ...issue, id: session.id })).toHaveLength(2);
    const names = [{ ...session, name: "Review [iOS] \\ notes" }, issue, agent];
    expect(readReferenceMessage(referenceMessage("Keep @literal and\nall text", names))).toEqual({ text: "Keep @literal and\nall text", references: names });
    expect(readReferenceMessage("[label](mention://session/%broken)\n\nbody")).toEqual({ text: "[label](mention://session/%broken)\n\nbody", references: [] });
    expect(readReferenceMessage("ordinary text")).toEqual({ text: "ordinary text", references: [] });
  });
});

describe("reference preparation", () => {
  it("sends canonical IDs, quoted snapshots and explicit agent recipients together", async () => {
    const source = resolver();
    const prepared = await prepareAIMessage("Please review", [session, issue, agent, agent], source);
    expect(prepared.references).toEqual([{ kind: "session", id: "session-1" }, { kind: "issue", id: "issue-1" }, { kind: "agent", id: "agent-1" }]);
    expect(prepared.agents).toEqual([{ id: "agent-1", name: "Reviewer" }]);
    expect(prepared.prompt).toContain("Referenced context (quoted data)");
    for (const value of ["Earlier question", "Earlier answer", "Task context", "comments, activity and attachment contents are not included"]) expect(prepared.prompt).toContain(value);
    expect(readReferenceMessage(prepared.text)).toEqual({ text: "Please review", references: [session, issue, agent] });
  });
  it("refuses cross-workspace, missing and unbound resources without dropping them", async () => {
    const source = resolver();
    const task = await source.issue(issue.id);
    source.issue = vi.fn(async () => ({ ...task, workspace_id: "other" }));
    await expect(prepareAIMessage("review", [issue], source)).rejects.toMatchObject({ reference: issue });
    source.agentCatalog = vi.fn(async () => ({ agents: [{ ...reviewer(), runtime_id: "" }], role: "owner" as const }));
    await expect(prepareAIMessage("review", [agent], source)).rejects.toMatchObject({ reference: agent });
    source.agentCatalog = vi.fn(async () => ({ agents: [], role: "owner" as const }));
    await expect(prepareAIMessage("review", [agent], source)).rejects.toMatchObject({ reference: agent });
  });
  it("bounds excerpts, labels incomplete history and leaves unreferenced sends unchanged", async () => {
    const source = resolver(), snapshot = await source.session(session.id);
    snapshot.turns[0].messages[0].content = "x".repeat(7000);
    snapshot.has_older_turns = true;
    source.session = vi.fn(async () => snapshot);
    const prepared = await prepareAIMessage("review", [session], source);
    expect(prepared.prompt).not.toContain("x".repeat(6001));
    expect(prepared.prompt).toContain("Earlier text omitted");
    expect(await prepareAIMessage("plain", [], source)).toEqual({ text: "plain", prompt: "plain", references: [], agents: [] });
  });
  it("matches invocation permissions rather than treating admins as private-agent owners", () => {
    const privateAgent = { ...reviewer(), permission_mode: "private" as const };
    expect(agentReferenceUnavailable(privateAgent, "someone-else", "owner")).toBe("no_permission");
    expect(agentReferenceUnavailable(privateAgent, "user-1", "member")).toBeNull();
  });
});
