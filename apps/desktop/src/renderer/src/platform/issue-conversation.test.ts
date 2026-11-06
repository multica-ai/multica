// @vitest-environment node
import { beforeEach, expect, it, vi } from "vitest";
import { launchIssueConversation } from "./issue-conversation";
import { issueConversationDraftKey, useIssueConversationStore } from "../stores/issue-conversation-store";

vi.mock("@multica/core/api", () => ({ api: { getBaseUrl: () => "https://api.test" } }));

const issue = { id: "issue-uuid", identifier: "CHOV-85", workspace_id: "workspace" };
const summary = { conversation_id: "new-session", summary_seq: 1, provider: "codex", title: "", status: "idle", pinned: false, archived: false, created_at: "now", updated_at: "now" };
function setup(deviceId = "local") {
  const service = {
    request: vi.fn(async (method: string): Promise<unknown> => method === "GET" ? { providers: [{ id: "codex", available: true }] } : summary),
    receiveSummary: vi.fn(),
  };
  return { issue, userId: "user", deviceId, service, isCurrent: vi.fn(() => true), open: vi.fn(), unavailableMessage: "No agent available" };
}
beforeEach(() => useIssueConversationStore.setState({ drafts: {}, pending: {} }));

it.each(["local", "peer-1"])("creates on %s before opening, with a scoped task mention in that session's draft", async (deviceId) => {
  const options = setup(deviceId);
  let finish!: (value: unknown) => void;
  options.service.request.mockImplementationOnce(async () => ({ providers: [{ id: "codex", available: true }] }));
  options.service.request.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const result = launchIssueConversation(options);
  await vi.waitFor(() => expect(options.service.request).toHaveBeenCalledWith("POST", "/sessions", expect.objectContaining({ provider: "codex" })));
  expect(options.open).not.toHaveBeenCalled();
  finish(summary);
  await result;
  expect(options.open).toHaveBeenCalledWith(expect.objectContaining(summary));
  const key = issueConversationDraftKey({ userId: "user", workspaceId: "workspace" }, deviceId, "new-session");
  const draft = useIssueConversationStore.getState().drafts[key];
  expect(draft).toMatchObject({ sessionId: "new-session", content: "[CHOV-85](mention://workspace-issue/issue-uuid?workspaceId=workspace) " });
  for (const other of [
    issueConversationDraftKey({ userId: "other", workspaceId: "workspace" }, deviceId, "new-session"),
    issueConversationDraftKey({ userId: "user", workspaceId: "other" }, deviceId, "new-session"),
    issueConversationDraftKey({ userId: "user", workspaceId: "workspace" }, `${deviceId}-other`, "new-session"),
    issueConversationDraftKey({ userId: "user", workspaceId: "workspace" }, deviceId, "existing-session"),
  ]) expect(useIssueConversationStore.getState().drafts[other]).toBeUndefined();
  useIssueConversationStore.getState().applied(key, "old-request");
  expect(useIssueConversationStore.getState().drafts[key]).toBe(draft);
  useIssueConversationStore.getState().applied(key, draft!.id);
  expect(useIssueConversationStore.getState().drafts[key]).toBeUndefined();
});

it("keeps the current page on creation failure and permits a retry", async () => {
  const options = setup();
  options.service.request.mockRejectedValueOnce(new Error("device_unreachable"));
  await expect(launchIssueConversation(options)).rejects.toThrow("device_unreachable");
  expect(options.open).not.toHaveBeenCalled();
  expect(useIssueConversationStore.getState().drafts).toEqual({});
  await launchIssueConversation(options);
  expect(options.open).toHaveBeenCalledOnce();
});

it("rejects unavailable or malformed providers without creating a session", async () => {
  for (const providers of [[], [{ id: "future", available: true }], [{ id: "codex", available: false }]]) {
    const options = setup();
    options.service.request.mockResolvedValueOnce({ providers });
    await expect(launchIssueConversation(options)).rejects.toThrow("No agent available");
    expect(options.service.request).toHaveBeenCalledTimes(1);
    expect(options.open).not.toHaveBeenCalled();
  }
});

it("does not duplicate creation while the menu is reopened", async () => {
  const options = setup();
  let finish!: (value: unknown) => void;
  options.service.request.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const first = launchIssueConversation(options);
  await launchIssueConversation({ ...options, deviceId: "peer-1" });
  expect(options.service.request).toHaveBeenCalledTimes(1);
  finish({ providers: [{ id: "claude", available: true }] });
  await first;
  expect(options.service.request).toHaveBeenLastCalledWith("POST", "/sessions", expect.objectContaining({ provider: "claude" }));
});

it("does not inject or navigate after the user changes account, workspace or task", async () => {
  const options = setup();
  options.service.request.mockImplementationOnce(async () => {
    options.isCurrent.mockReturnValue(false);
    return { providers: [{ id: "codex", available: true }] };
  });
  await launchIssueConversation(options);
  expect(options.service.request).toHaveBeenCalledTimes(1);
  expect(options.open).not.toHaveBeenCalled();
  expect(useIssueConversationStore.getState().drafts).toEqual({});
});

it("ignores a late create response after navigation changes", async () => {
  const options = setup();
  options.service.request.mockImplementationOnce(async () => ({ providers: [{ id: "codex", available: true }] }));
  options.service.request.mockImplementationOnce(async () => { options.isCurrent.mockReturnValue(false); return summary; });
  await launchIssueConversation(options);
  expect(options.open).not.toHaveBeenCalled();
  expect(useIssueConversationStore.getState().drafts).toEqual({});
});
