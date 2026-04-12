import { beforeEach, describe, expect, it } from "vitest";
import type { StorageAdapter } from "../types";
import { setCurrentWorkspaceId } from "../platform/workspace-storage";
import {
  PING_RECOVERY_WINDOW_MS,
  PING_TERMINAL_TTL_MS,
  UPDATE_COMPLETED_TTL_MS,
  UPDATE_RECOVERY_WINDOW_MS,
  createDaemonUpdateStore,
  createRuntimePingStore,
} from "./store";

function createMemoryStorage(): StorageAdapter {
  const data = new Map<string, string>();

  return {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => {
      data.set(key, value);
    },
    removeItem: (key) => {
      data.delete(key);
    },
  };
}

describe("runtime ping store", () => {
  beforeEach(() => {
    setCurrentWorkspaceId(null);
  });

  it("writes, updates, clears, and cleans up ping entries", () => {
    const store = createRuntimePingStore(createMemoryStorage());

    store.getState().setEntry({
      runtimeId: "runtime-1",
      requestId: "ping-1",
      status: "running",
      startedAt: 1_000,
    });

    expect(store.getState().entries["runtime-1"]).toMatchObject({
      requestId: "ping-1",
      status: "running",
    });

    store.getState().updateEntry("runtime-1", {
      status: "completed",
      output: "pong",
      finishedAt: 2_000,
      durationMs: 120,
    });

    expect(store.getState().entries["runtime-1"]).toMatchObject({
      status: "completed",
      output: "pong",
      durationMs: 120,
      finishedAt: 2_000,
    });

    store.getState().cleanupExpired(2_000 + PING_TERMINAL_TTL_MS + 1);
    expect(store.getState().entries["runtime-1"]).toBeUndefined();

    store.getState().setEntry({
      runtimeId: "runtime-2",
      requestId: "ping-2",
      status: "running",
      startedAt: 0,
    });
    store.getState().cleanupExpired(PING_RECOVERY_WINDOW_MS + 1);

    expect(store.getState().entries["runtime-2"]).toMatchObject({
      status: "timeout",
      finishedAt: PING_RECOVERY_WINDOW_MS + 1,
    });

    store.getState().clearEntry("runtime-2");
    expect(store.getState().entries["runtime-2"]).toBeUndefined();
  });

  it("isolates persisted ping entries by workspace", async () => {
    const storage = createMemoryStorage();

    setCurrentWorkspaceId("ws-a");
    const store = createRuntimePingStore(storage);
    store.getState().setEntry({
      runtimeId: "runtime-a",
      requestId: "ping-a",
      status: "completed",
      startedAt: 100,
      finishedAt: 200,
      output: "pong",
    });

    setCurrentWorkspaceId("ws-b");
    await store.persist.rehydrate();
    expect(store.getState().entries).toEqual({});

    store.getState().setEntry({
      runtimeId: "runtime-b",
      requestId: "ping-b",
      status: "failed",
      startedAt: 300,
      finishedAt: 400,
      error: "boom",
    });

    setCurrentWorkspaceId("ws-a");
    await store.persist.rehydrate();
    expect(store.getState().entries["runtime-a"]).toMatchObject({
      requestId: "ping-a",
      output: "pong",
    });
    expect(store.getState().entries["runtime-b"]).toBeUndefined();
  });
});

describe("daemon update store", () => {
  it("shares update entries by daemon ID and preserves identifiers", () => {
    const store = createDaemonUpdateStore(createMemoryStorage());

    store.getState().setEntry({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.3",
      status: "running",
      startedAt: 1_000,
    });

    expect(store.getState().entries["daemon-1"]).toMatchObject({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.3",
      status: "running",
    });

    store.getState().setEntry({
      daemonId: "daemon-2",
      runtimeId: "runtime-c",
      requestId: "update-2",
      targetVersion: "v2.0.0",
      status: "failed",
      startedAt: 2_000,
      finishedAt: 3_000,
      error: "failed",
    });

    expect(store.getState().entries["daemon-1"]?.runtimeId).toBe("runtime-a");
    expect(store.getState().entries["daemon-2"]?.requestId).toBe("update-2");
  });

  it("applies update cleanup rules by status", () => {
    const store = createDaemonUpdateStore(createMemoryStorage());

    store.getState().setEntry({
      daemonId: "daemon-complete",
      runtimeId: "runtime-a",
      requestId: "update-complete",
      targetVersion: "v1.2.3",
      status: "completed",
      startedAt: 0,
      finishedAt: 10,
      output: "done",
    });
    store.getState().setEntry({
      daemonId: "daemon-failed",
      runtimeId: "runtime-b",
      requestId: "update-failed",
      targetVersion: "v1.2.3",
      status: "failed",
      startedAt: 0,
      finishedAt: 10,
      error: "nope",
    });
    store.getState().setEntry({
      daemonId: "daemon-stale",
      runtimeId: "runtime-c",
      requestId: "update-stale",
      targetVersion: "v1.2.3",
      status: "interrupted",
      startedAt: 0,
      error: "network lost",
    });

    store.getState().cleanupExpired(UPDATE_RECOVERY_WINDOW_MS + 1);
    expect(store.getState().entries["daemon-stale"]).toMatchObject({
      status: "timeout",
      finishedAt: UPDATE_RECOVERY_WINDOW_MS + 1,
    });
    expect(store.getState().entries["daemon-failed"]?.status).toBe("failed");

    store.getState().cleanupExpired(UPDATE_COMPLETED_TTL_MS + 11);
    expect(store.getState().entries["daemon-complete"]).toBeUndefined();
    expect(store.getState().entries["daemon-failed"]?.status).toBe("failed");
  });
});
