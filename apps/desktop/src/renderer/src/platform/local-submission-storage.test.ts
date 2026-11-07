// @vitest-environment node
import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { beforeAll, expect, it } from "vitest";
import { createSubmissionStorage } from "./local-submission-storage";
import type { StoredSubmission } from "@multica/core/local-conversation/submission-storage";
beforeAll(() => {
  globalThis.IDBKeyRange = IDBKeyRange;
});
const record = (id: string): StoredSubmission => ({
  version: 1,
  requestId: id,
  sessionId: "s",
  sequence: 0,
  createdAt: 1,
  kind: "prompt",
  stage: "unconfirmed",
  input: { content: [{ type: "text", text: id }] },
  generation: "g",
  lifecycle: true,
});
it("survives reopening, retains ordering and isolates accounts, origins and devices", async () => {
  const factory = new IDBFactory();
  const first = createSubmissionStorage(
    "origin/account/workspace/device",
    factory,
  );
  await Promise.all([first.put(record("a")), first.put(record("b"))]);
  const reopened = createSubmissionStorage(
    "origin/account/workspace/device",
    factory,
  );
  expect(
    (await reopened.read()).map((value) => [value.requestId, value.sequence]),
  ).toEqual([
    ["a", 1],
    ["b", 2],
  ]);
  expect(
    await createSubmissionStorage(
      "other-origin/account/workspace/device",
      factory,
    ).read(),
  ).toEqual([]);
  await reopened.confirm("a");
  await reopened.put(record("a"));
  expect((await first.read())[0].stage).toBe("confirmed");
  await reopened.remove("a");
  expect((await first.read()).map((value) => value.requestId)).toEqual(["b"]);
});
it("rejects missing storage instead of silently accepting a send", async () => {
  await expect(
    createSubmissionStorage("scope", undefined).put(record("a")),
  ).rejects.toThrow("unavailable");
});

it("preserves a prepared member reference when reopening a submission", async () => {
  const factory = new IDBFactory();
  const store = createSubmissionStorage("member-scope", factory);
  const value = record("member-send");
  value.prepared = {
    requestId: value.requestId,
    content: "[@Alex](mention://workspace-member/user-b?workspaceId=ws&userId=user-b)",
    references: [{ kind: "member", id: "user-b", workspaceId: "ws" }],
    display: { id: value.requestId, createdAtUnixMs: 1, content: [{ type: "text", text: "Alex" }] },
  };
  await store.put(value);
  const reopened = createSubmissionStorage("member-scope", factory);
  expect((await reopened.read())[0].prepared).toEqual(value.prepared);
});
it("does not let a late authority downgrade a newer mirror", async () => {
  const store = createSubmissionStorage("scope", new IDBFactory());
  await store.put(record("a"));
  const authority = {
    request_id: "a",
    revision: 7,
    state: "removed" as const,
    item: {},
    position: null,
    blocked_reason: "",
    message_id: "",
    attempt: null,
  };
  await store.confirm("a", authority);
  await store.confirm("a", { ...authority, revision: 6, state: "queued" });
  await store.put(record("a"));
  expect((await store.read())[0]).toMatchObject({
    stage: "confirmed",
    authority: { revision: 7, state: "removed" },
  });
});
