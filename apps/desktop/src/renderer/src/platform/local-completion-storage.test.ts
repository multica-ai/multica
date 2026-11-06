// @vitest-environment node
import { IDBFactory } from "fake-indexeddb";
import { expect, it } from "vitest";
import type { CompletionReadRecord } from "@multica/conversation-client/models";
import { createCompletionReadStorage } from "./local-completion-storage";

const record: CompletionReadRecord = {
  version: 1, generation: "g", baselineTurnIndex: 0, summarySeq: 2, turnCount: 1, activeTurnId: null, status: "idle",
  completedTurnIndex: 0, completedTurnId: "t0", readTurnIndex: -1,
};

it("restores durable positions and isolates origins, accounts, workspaces, devices and sessions", async () => {
  const factory = new IDBFactory();
  const namespace = JSON.stringify(["origin", "user", "workspace", "local"]);
  await createCompletionReadStorage(() => namespace, factory).write("c", record);
  const reopened = createCompletionReadStorage(() => namespace, factory);
  expect(await reopened.read("c")).toEqual(record);
  expect(await reopened.read("other-session")).toBeNull();
  for (let i = 0; i < 4; i++) {
    const parts = ["origin", "user", "workspace", "local"]; parts[i] = "other";
    expect(await createCompletionReadStorage(() => JSON.stringify(parts), factory).read("c")).toBeNull();
  }
});

it("cannot regress a completion or read position with a late write from another window", async () => {
  const factory = new IDBFactory();
  const a = createCompletionReadStorage(() => "scope", factory), b = createCompletionReadStorage(() => "scope", factory);
  await a.write("c", record);
  await b.write("c", { ...record, readTurnIndex: 0 });
  await a.write("c", { ...record, summarySeq: 3, turnCount: 2, completedTurnIndex: 1, completedTurnId: "t1" });
  await b.write("c", record);
  expect(await a.read("c")).toMatchObject({ completedTurnIndex: 1, completedTurnId: "t1", readTurnIndex: 0, summarySeq: 3 });
  await a.write("c", { ...record, generation: "replacement" });
  expect(await a.read("c")).toEqual({ ...record, generation: "replacement" });
});

it("rejects unavailable storage", async () => {
  await expect(createCompletionReadStorage(() => "scope", undefined).write("c", record)).rejects.toThrow("unavailable");
});
