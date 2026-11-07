// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { Issue, UpdateIssueRequest } from "../types";
import { queueIssueDescriptionSave, type DescriptionDraft } from "./description-save";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const draft = (markdown: string, attachmentIds: string[] = []): DescriptionDraft => ({
  markdown, baseMarkdown: "adopted baseline", attachmentIds,
});
const accepted = (description: string) => ({ description } as Issue);
const settled = async () => { await Promise.resolve(); await Promise.resolve(); };

describe("active description saves", () => {
  it("keeps only the latest unsent draft, attachments and submitting editor", async () => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const oldSave = vi.fn().mockReturnValueOnce(first.promise);
    const newSave = vi.fn().mockResolvedValue(accepted("D2c"));
    const onError = vi.fn();
    const queue = (d: DescriptionDraft, save = oldSave) =>
      queueIssueDescriptionSave(client, "ws", "issue", d, save, onError);

    queue(draft("D1"));
    queue(draft("D2a", ["deleted"]));
    queue(draft("D2b", ["removed"]));
    queue(draft("D2c", ["kept"]), newSave);
    expect(oldSave).toHaveBeenCalledTimes(1);
    expect(newSave).not.toHaveBeenCalled();
    first.resolve(accepted("D1"));
    await settled();
    expect(newSave).toHaveBeenCalledExactlyOnceWith({
      id: "issue", description: "D2c", description_base: "D1", attachment_ids: ["kept"],
    });
    expect(onError).not.toHaveBeenCalled();
  });

  it("uses submitted text as the next media baseline, leaving late server media unseen", async () => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const save = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue(accepted("D2"));
    queueIssueDescriptionSave(client, "ws", "issue", draft("D1"), save, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("D2"), save, vi.fn());
    first.resolve(accepted("D1\n\n![late channel media](/api/attachments/late/download)"));
    await settled();
    expect(save).toHaveBeenNthCalledWith(2, {
      id: "issue", description: "D2", description_base: "D1", attachment_ids: undefined,
    });
  });

  it("does not rebind attachments removed from the latest queued document", async () => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const save = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue(accepted(""));
    queueIssueDescriptionSave(client, "ws", "issue", draft("![upload](/upload)", ["upload"]), save, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft(""), save, vi.fn());
    first.resolve(accepted("![upload](/upload)"));
    await settled();
    expect(save).toHaveBeenNthCalledWith(2, {
      id: "issue", description: "", description_base: "![upload](/upload)", attachment_ids: undefined,
    });
  });

  it("keeps an empty submitted baseline for the next queued edit", async () => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const save = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue(accepted("next"));
    queueIssueDescriptionSave(client, "ws", "issue", draft(""), save, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("next"), save, vi.fn());
    first.resolve(accepted(""));
    await settled();
    expect(save).toHaveBeenNthCalledWith(2, expect.objectContaining({ description_base: "" }));
  });

  it("lets a reopened editor's D3 replace closed-editor D2 before the first save completes", async () => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const saveClosed = vi.fn().mockReturnValueOnce(first.promise);
    const saveReopened = vi.fn().mockResolvedValue(accepted("D3"));
    queueIssueDescriptionSave(client, "ws", "issue", draft("D1"), saveClosed, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("D2"), saveClosed, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("D3"), saveReopened, vi.fn());
    first.resolve(accepted("D1"));
    await settled();
    expect(saveClosed).toHaveBeenCalledTimes(1);
    expect(saveReopened).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ description: "D3" }));
  });

  it("serializes live D3 behind an already sent D2 instead of replaying D2 after it", async () => {
    const client = new QueryClient();
    const second = deferred<Issue>();
    const save = vi.fn().mockResolvedValueOnce(accepted("D1"))
      .mockReturnValueOnce(second.promise).mockResolvedValue(accepted("D3"));
    queueIssueDescriptionSave(client, "ws", "issue", draft("D1"), save, vi.fn());
    queueIssueDescriptionSave(client, "ws", "issue", draft("D2"), save, vi.fn());
    await settled();
    queueIssueDescriptionSave(client, "ws", "issue", draft("D3"), save, vi.fn());
    expect(save).toHaveBeenCalledTimes(2);
    second.resolve(accepted("D2"));
    await settled();
    expect(save).toHaveBeenNthCalledWith(3, expect.objectContaining({ description: "D3", description_base: "D2" }));
  });

  it.each(["issue", "workspace", "QueryClient"])("does not share a queue across %s identity", async (axis) => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const save = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue(accepted("other"));
    queueIssueDescriptionSave(client, "ws", "issue", draft("D1"), save, vi.fn());
    queueIssueDescriptionSave(axis === "QueryClient" ? new QueryClient() : client,
      axis === "workspace" ? "other-ws" : "ws", axis === "issue" ? "other-issue" : "issue",
      draft("other"), save, vi.fn());
    expect(save).toHaveBeenCalledTimes(2);
    first.resolve(accepted("D1"));
    await settled();
  });

  it("drops failed pending work without retry and accepts the next user edit with its own baseline", async () => {
    const client = new QueryClient();
    const first = deferred<Issue>();
    const error = new Error("synthetic transport failure");
    const onError = vi.fn();
    const save = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue(accepted("next"));
    const queue = (markdown: string) => queueIssueDescriptionSave(client, "ws", "issue", draft(markdown), save, onError);
    queue("D1"); queue("D2");
    first.reject(error);
    await settled();
    expect(save).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledExactlyOnceWith(error);
    queue("next");
    await settled();
    expect(save).toHaveBeenNthCalledWith(2, expect.objectContaining({ description: "next", description_base: "adopted baseline" }));
  });

  it("releases a successful queue so a later independent edit keeps its adopted baseline", async () => {
    const client = new QueryClient();
    const save = vi.fn<(input: UpdateIssueRequest & { id: string }) => Promise<Issue>>().mockResolvedValue(accepted("accepted"));
    queueIssueDescriptionSave(client, "ws", "issue", draft("D1"), save, vi.fn());
    await settled();
    queueIssueDescriptionSave(client, "ws", "issue", { ...draft("later"), baseMarkdown: "external update" }, save, vi.fn());
    await settled();
    expect(save).toHaveBeenNthCalledWith(2, expect.objectContaining({ description_base: "external update" }));
  });
});
