// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { aiDraftKey, useAIDrafts, type PendingSend } from "./drafts";
vi.mock("expo-secure-store", () => ({ getItemAsync: vi.fn(async () => null), setItemAsync: vi.fn(async () => {}), deleteItemAsync: vi.fn(async () => {}) }));

const scope = { userId: "user", workspaceId: "workspace", deviceId: "device" };
const key = aiDraftKey(scope, "session");
const reference = { kind: "agent" as const, id: "agent-1", name: "Reviewer" };
const request: PendingSend = { requestId: "request-1", text: "Review", attachments: [], prepared: { text: "Review", prompt: "Review", references: [{ kind: "agent", id: "agent-1" }], agents: [{ id: "agent-1", name: "Reviewer" }] } };
beforeEach(() => useAIDrafts.setState({ drafts: {}, references: {}, attachments: {}, pending: {} }));
describe("AI reference drafts", () => {
  it("persists prepared references together with uploaded attachments while dropping unfinished transfers", () => {
    const store = useAIDrafts.getState();
    store.setDraft(key, "Review");
    store.setReferences(key, [reference]);
    store.setPending(key, request);
    store.setAttachments(key, [
      { id: "stored", name: "Stored file", status: "uploaded" },
      { id: "in-flight", name: "Uploading file", status: "uploading" },
      { id: "failed", name: "Failed file", status: "failed" },
    ]);
    const persisted = useAIDrafts.persist.getOptions().partialize!(useAIDrafts.getState());
    expect(persisted.references[key]).toEqual([reference]);
    expect(persisted.pending[key]).toEqual(request);
    expect(persisted.attachments[key]).toEqual([{ id: "stored", name: "Stored file", status: "uploaded" }]);
  });
  it("isolates references by account, workspace, device and conversation", () => {
    useAIDrafts.getState().setReferences(key, [reference]);
    for (const other of [aiDraftKey({ ...scope, userId: "other" }, "session"), aiDraftKey({ ...scope, workspaceId: "other" }, "session"), aiDraftKey({ ...scope, deviceId: "other" }, "session"), aiDraftKey(scope, "other")]) expect(useAIDrafts.getState().references[other]).toBeUndefined();
  });
  it("retains the complete draft until acknowledged and clears the matching send together", () => {
    const store = useAIDrafts.getState();
    store.setDraft(key, "Review"); store.setReferences(key, [reference]); store.setPending(key, request);
    expect(useAIDrafts.getState()).toMatchObject({ drafts: { [key]: "Review" }, references: { [key]: [reference] }, pending: { [key]: request } });
    store.complete(key, { ...request, requestId: "old-request" });
    expect(useAIDrafts.getState().pending[key]).toEqual(request);
    store.complete(key, request);
    expect(useAIDrafts.getState().drafts[key]).toBeUndefined();
    expect(useAIDrafts.getState().references[key]).toBeUndefined();
    expect(useAIDrafts.getState().pending[key]).toBeUndefined();
  });
  it("does not erase a newer text draft or references on a late acknowledgement", () => {
    const store = useAIDrafts.getState();
    store.setDraft(key, "New draft"); store.setReferences(key, [{ ...reference, id: "agent-2" }]); store.setPending(key, request);
    store.complete(key, request);
    expect(useAIDrafts.getState().drafts[key]).toBe("New draft");
    expect(useAIDrafts.getState().references[key][0].id).toBe("agent-2");
  });
  it("clears only submitted attachments and preserves newer file selections", () => {
    const store = useAIDrafts.getState();
    const withFile = { ...request, attachments: ["file-1"] };
    store.setAttachments(key, [{ id: "file-1", name: "Screenshot", status: "uploaded" }, { id: "file-2", name: "New file", status: "uploaded" }]);
    store.setPending(key, withFile);
    store.complete(key, withFile);
    expect(useAIDrafts.getState().attachments[key]).toEqual([{ id: "file-2", name: "New file", status: "uploaded" }]);
  });
});
