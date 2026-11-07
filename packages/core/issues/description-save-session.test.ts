// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { Issue, StorageAdapter } from "../types";
import { clearClientSessionData } from "../platform/session-cleanup";
import {
  getIssueDescriptionSaveSession,
  queueIssueDescriptionSave,
} from "./description-save-queue";

const storage: StorageAdapter = {
  getItem: () => null, setItem: () => {}, removeItem: () => {}, keys: () => [],
};
const draft = (markdown: string) => ({ markdown, baseMarkdown: "baseline", attachmentIds: [] });
const accepted = (description: string) => ({ description } as Issue);
const settled = async () => { await Promise.resolve(); await Promise.resolve(); };
function deferred() {
  let resolve!: (issue: Issue) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<Issue>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

// Exercise the cleanup shared by explicit logout and rejected-session teardown,
// including pending saves that outlive both the editor and the Query cache.
describe("description saves across session cleanup", () => {
  it.each(["success", "failure"])("drops old pending work on late %s and keeps the new session's queue", async (outcome) => {
    const client = new QueryClient();
    const old = deferred();
    const current = deferred();
    const oldError = vi.fn();
    const oldSave = vi.fn().mockReturnValueOnce(old.promise);
    const newSave = vi.fn().mockReturnValueOnce(current.promise).mockResolvedValue(accepted("B2"));
    queueIssueDescriptionSave(client, "ws", "issue", draft("A1"), oldSave, oldError);
    queueIssueDescriptionSave(client, "ws", "issue", draft("A2"), oldSave, oldError);
    clearClientSessionData(client, storage);
    queueIssueDescriptionSave(client, "ws", "issue", draft("B1"), newSave, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("B2"), newSave, vi.fn());
    expect(newSave).toHaveBeenCalledTimes(1);
    if (outcome === "success") old.resolve(accepted("A1"));
    else old.reject(new Error("old session failure"));
    await settled();
    expect(oldSave).toHaveBeenCalledTimes(1);
    expect(oldError).not.toHaveBeenCalled();
    // This edit must join B's existing queue, even after A's finally runs.
    queueIssueDescriptionSave(client, "ws", "issue", draft("B3"), newSave, vi.fn());
    expect(newSave).toHaveBeenCalledTimes(1);
    current.resolve(accepted("B1"));
    await settled();
    expect(newSave).toHaveBeenNthCalledWith(2, expect.objectContaining({
      description: "B3", description_base: "B1",
    }));
    expect(newSave).toHaveBeenCalledTimes(2);
  });

  it("does not resume a queue after logout without a subsequent login", async () => {
    const client = new QueryClient();
    const first = deferred();
    const save = vi.fn().mockReturnValueOnce(first.promise);
    queueIssueDescriptionSave(client, "ws", "issue", draft("A1"), save, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("A2"), save, vi.fn());
    clearClientSessionData(client, storage);
    clearClientSessionData(client, storage);
    first.resolve(accepted("A1"));
    await settled();
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("rejects a closed editor's late flush even after a new session starts", async () => {
    const client = new QueryClient();
    const session = getIssueDescriptionSaveSession(client);
    const save = vi.fn().mockResolvedValue(accepted("saved"));
    clearClientSessionData(client, storage);
    queueIssueDescriptionSave(client, "ws", "issue", draft("B"), save, vi.fn());
    await settled();
    queueIssueDescriptionSave(client, "ws", "issue", draft("late A flush"), save, vi.fn(), session);
    await settled();
    expect(save).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ description: "B" }));
  });

  it("invalidates every workspace and issue owned by the ended client session", async () => {
    const client = new QueryClient();
    const first = deferred();
    const save = vi.fn().mockReturnValue(first.promise);
    for (const [ws, issue] of [["ws", "issue"], ["other-ws", "issue"], ["ws", "other-issue"]] as const) {
      queueIssueDescriptionSave(client, ws, issue, draft("A1"), save, vi.fn());
      queueIssueDescriptionSave(client, ws, issue, draft("A2"), save, vi.fn());
    }
    clearClientSessionData(client, storage);
    first.resolve(accepted("A1"));
    await settled();
    expect(save).toHaveBeenCalledTimes(3);
  });

  it("does not end another QueryClient's session", async () => {
    const client = new QueryClient();
    const other = new QueryClient();
    const first = deferred();
    const save = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue(accepted("other2"));
    queueIssueDescriptionSave(other, "ws", "issue", draft("other1"), save, vi.fn());
    queueIssueDescriptionSave(other, "ws", "issue", draft("other2"), save, vi.fn());
    clearClientSessionData(client, storage);
    first.resolve(accepted("other1"));
    await settled();
    expect(save).toHaveBeenNthCalledWith(2, expect.objectContaining({ description: "other2" }));
  });
});
