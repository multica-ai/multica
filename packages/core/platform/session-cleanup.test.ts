/**
 * @vitest-environment jsdom
 */
// jsdom rather than node: the cleanup writes an expiring `document.cookie`,
// and under node it would take the `typeof document === "undefined"` branch
// and pass without ever exercising that line.
import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  registerDraftCleanup,
  __clearDraftCleanupRegistryForTest,
} from "../drafts/cleanup-registry";
import type { StorageAdapter, Workspace } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { clearClientSessionData } from "./session-cleanup";

function makeStorage(
  initial: Record<string, string> = {},
): StorageAdapter & { snapshot: () => Record<string, string> } {
  const values = { ...initial };
  return {
    getItem: (key) => values[key] ?? null,
    setItem: (key, value) => {
      values[key] = value;
    },
    removeItem: (key) => {
      delete values[key];
    },
    keys: () => Object.keys(values),
    snapshot: () => ({ ...values }),
  };
}

beforeEach(() => {
  __clearDraftCleanupRegistryForTest();
  document.cookie = "last_workspace_slug=acme; path=/";
});

describe("clearClientSessionData", () => {
  // The scenario this exists for: user A's session dies, the login form the
  // expiry lands on is used by B, and A and B share a workspace — so the
  // stale-tab validator that runs after login finds nothing to prune and
  // every one of these keys would otherwise still be A's.
  it("leaves nothing of the previous session for the next user to find", () => {
    const shared = "acme";
    const storage = makeStorage({
      // A workspace-scoped draft, through the registry.
      "multica_comment_drafts:acme": '{"issue-1":"A private draft"}',
      // A non-draft workspace-scoped key.
      "multica:chat:activeSessionId:acme": "session-1",
      // Desktop tab layout, whose paths carry slugs and issue ids.
      multica_tabs: '[{"path":"/acme/issues/secret-issue"}]',
      // Untouched: not owned by the session.
      multica_locale: "zh-Hans",
    });
    const resetInMemory = vi.fn();
    registerDraftCleanup({
      storageKey: "multica_comment_drafts",
      workspaceScoped: true,
      resetInMemory,
    });

    const queryClient = new QueryClient();
    queryClient.setQueryData(workspaceKeys.list(), [
      { id: "ws-1", slug: shared },
    ] as Workspace[]);
    queryClient.setQueryData(["issues", "ws-1"], [{ id: "issue-1" }]);

    clearClientSessionData(queryClient, storage);

    // Persisted layer.
    expect(storage.snapshot()).toEqual({ multica_locale: "zh-Hans" });
    // Memory layer — a client-side login does not reload the page, so the
    // Zustand singleton would otherwise still hold A's draft.
    expect(resetInMemory).toHaveBeenCalledOnce();
    // Server-state layer. Every query has staleTime: Infinity, so anything
    // left here would render for B and never refetch away.
    expect(queryClient.getQueryData(["issues", "ws-1"])).toBeUndefined();
    expect(queryClient.getQueryData(workspaceKeys.list())).toBeUndefined();
    expect(document.cookie).not.toContain("last_workspace_slug=acme");
  });

  it("clears every workspace the session had, not just the active one", () => {
    const storage = makeStorage({
      "multica_navigation:acme": "1",
      "multica_navigation:globex": "2",
    });
    const queryClient = new QueryClient();
    queryClient.setQueryData(workspaceKeys.list(), [
      { id: "ws-1", slug: "acme" },
      { id: "ws-2", slug: "globex" },
    ] as Workspace[]);

    clearClientSessionData(queryClient, storage);

    expect(storage.snapshot()).toEqual({});
  });

  // Each in-memory reset is a Zustand setState, and persist middleware writes
  // the reset state straight back to storage under the still-active workspace
  // slug. Resetting AFTER the per-slug removal therefore resurrects the keys
  // just deleted — for the issue draft store, with the previous user's
  // lastAssignee inside.
  it("resets in-memory drafts before removing their persisted keys", () => {
    const storage = makeStorage({ "multica_issue_draft:acme": "A's draft" });
    registerDraftCleanup({
      storageKey: "multica_issue_draft",
      workspaceScoped: true,
      // Stands in for persist middleware writing the emptied store back out.
      resetInMemory: () =>
        storage.setItem("multica_issue_draft:acme", '{"lastAssignee":"A"}'),
    });
    const queryClient = new QueryClient();
    queryClient.setQueryData(workspaceKeys.list(), [
      { id: "ws-1", slug: "acme" },
    ] as Workspace[]);

    clearClientSessionData(queryClient, storage);

    expect(storage.snapshot()["multica_issue_draft:acme"]).toBeUndefined();
  });

  // A cold start rejected at the identity probe has an empty Query cache, so
  // there is no workspace list to read slugs from. Asserting only the global
  // `multica_tabs` here would pass while every per-workspace key survived —
  // the exact hole that let a stale-token launch leak A's drafts to B.
  it("clears workspace-scoped keys even with no workspace list to enumerate", () => {
    const storage = makeStorage({
      "multica_comment_drafts:acme": '{"issue-1":"A private draft"}',
      "multica:chat:activeSessionId:acme": "session-1",
      multica_tabs: "[]",
    });
    registerDraftCleanup({
      storageKey: "multica_comment_drafts",
      workspaceScoped: true,
      resetInMemory: vi.fn(),
    });
    const queryClient = new QueryClient();
    expect(queryClient.getQueryData(workspaceKeys.list())).toBeUndefined();

    clearClientSessionData(queryClient, storage);

    expect(storage.snapshot()).toEqual({});
  });
});
