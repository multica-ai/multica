import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApiClient } from "../api/client";
import type { Attachment } from "../types";
import {
  startUpload,
  abortAll,
  abortUpload,
  __trackedUploadCountForTest,
  type UploadOutcome,
} from "./upload-coordinator";

function makeAttachment(id: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "alice",
    filename: `${id}.png`,
    url: `https://cdn.example.test/${id}.png`,
    download_url: `https://cdn.example.test/${id}.png`,
    markdown_url: `https://app.example.test/api/attachments/${id}/download`,
    content_type: "image/png",
    size_bytes: 10,
    created_at: "2026-06-12T00:00:00Z",
  };
}

const file = () => new File(["x"], "shot.png", { type: "image/png" });
const tick = () => new Promise((r) => setTimeout(r, 0));

/** An api whose uploadFile resolves/rejects/aborts under the test's control. */
function abortableApi(): {
  api: Pick<ApiClient, "uploadFile">;
  resolve: (att: Attachment) => void;
  reject: (err: Error) => void;
  sawSignal: () => AbortSignal | undefined;
} {
  let resolveFn: (att: Attachment) => void = () => {};
  let rejectFn: (err: Error) => void = () => {};
  let captured: AbortSignal | undefined;
  const uploadFile = vi.fn(
    (_f: File, _opts?: unknown, signal?: AbortSignal): Promise<Attachment> => {
      captured = signal;
      return new Promise<Attachment>((resolve, reject) => {
        resolveFn = resolve;
        // Reject with an AbortError the moment the signal fires — this is what
        // the real `fetch` does on abort.
        signal?.addEventListener("abort", () => {
          const err = new Error("aborted");
          err.name = "AbortError";
          reject(err);
        });
        rejectFn = reject;
      });
    },
  );
  return {
    api: { uploadFile } as unknown as Pick<ApiClient, "uploadFile">,
    resolve: (att) => resolveFn(att),
    reject: (err) => rejectFn(err),
    sawSignal: () => captured,
  };
}

afterEach(() => {
  abortAll();
});

describe("upload coordinator", () => {
  it("delivers the attachment on success and stops tracking", async () => {
    const { api, resolve } = abortableApi();
    const settled: UploadOutcome[] = [];

    startUpload({
      clientUploadId: "c1",
      file: file(),
      api,
      ctx: { issueId: "issue-1" },
      onSettled: (o) => settled.push(o),
    });
    expect(__trackedUploadCountForTest()).toBe(1);

    resolve(makeAttachment("att-1"));
    await tick();

    expect(settled).toHaveLength(1);
    expect(settled[0]).toMatchObject({ clientUploadId: "c1", status: "uploaded" });
    expect(settled[0]).toHaveProperty("attachment.id", "att-1");
    expect(__trackedUploadCountForTest()).toBe(0);
  });

  it("forwards the upload context to api.uploadFile", async () => {
    const { api } = abortableApi();
    startUpload({
      clientUploadId: "c1",
      file: file(),
      api,
      ctx: { commentId: "cmt-9", chatSessionId: "sess-3" },
      onSettled: () => {},
    });
    expect(api.uploadFile).toHaveBeenCalledWith(
      expect.any(File),
      { issueId: undefined, commentId: "cmt-9", chatSessionId: "sess-3" },
      expect.any(AbortSignal),
    );
  });

  it("reports a non-abort error as failed", async () => {
    const { api, reject } = abortableApi();
    const settled: UploadOutcome[] = [];

    startUpload({ clientUploadId: "c1", file: file(), api, onSettled: (o) => settled.push(o) });
    reject(new Error("boom"));
    await tick();

    expect(settled).toHaveLength(1);
    expect(settled[0]).toMatchObject({ clientUploadId: "c1", status: "failed" });
    expect((settled[0] as { error: Error }).error.message).toBe("boom");
    expect(__trackedUploadCountForTest()).toBe(0);
  });

  it("does NOT call onSettled when aborted", async () => {
    const { api } = abortableApi();
    const settled: UploadOutcome[] = [];

    startUpload({ clientUploadId: "c1", file: file(), api, onSettled: (o) => settled.push(o) });
    abortUpload("c1");
    await tick();

    expect(settled).toHaveLength(0);
    expect(__trackedUploadCountForTest()).toBe(0);
  });

  it("aborts every tracked upload on abortAll and settles none", async () => {
    const a = abortableApi();
    const b = abortableApi();
    const settled: UploadOutcome[] = [];

    startUpload({ clientUploadId: "c1", file: file(), api: a.api, onSettled: (o) => settled.push(o) });
    startUpload({ clientUploadId: "c2", file: file(), api: b.api, onSettled: (o) => settled.push(o) });
    expect(__trackedUploadCountForTest()).toBe(2);

    abortAll();
    await tick();

    expect(settled).toHaveLength(0);
    expect(__trackedUploadCountForTest()).toBe(0);
    expect(a.sawSignal()?.aborted).toBe(true);
    expect(b.sawSignal()?.aborted).toBe(true);
  });
});
