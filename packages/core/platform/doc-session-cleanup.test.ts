// @vitest-environment jsdom
import { QueryClient } from "@tanstack/react-query";
import { afterEach, expect, it } from "vitest";
import { clearClientSessionData } from "./session-cleanup";
import { setCurrentWorkspace } from "./workspace-storage";

afterEach(() => {
  setCurrentWorkspace(null, null);
  localStorage.clear();
});

it("clears document drafts on an expired-session cold start before opening Docs", async () => {
  // Do not import the Docs store here: cleanup must preload it itself.
  const privateDraft = JSON.stringify({
    state: {
      draft: { entries: { "ws:doc": { content: "Previous user's draft" } } },
    },
    version: 0,
  });
  localStorage.setItem("doc-drafts:acme", privateDraft);
  localStorage.setItem("doc-drafts:globex", privateDraft);
  localStorage.setItem("multica_locale", "zh-Hans");
  const client = new QueryClient(); // No cached workspace list at cold start.

  clearClientSessionData(client);

  expect(localStorage.getItem("doc-drafts:acme")).toBeNull();
  expect(localStorage.getItem("doc-drafts:globex")).toBeNull();
  expect(localStorage.getItem("multica_locale")).toBe("zh-Hans");

  // A second account entering the same workspace cannot hydrate A's draft.
  setCurrentWorkspace("acme", "ws");
  const { useDocDraftStore } = await import("../docs/draft-store");
  expect(useDocDraftStore.getState().draft.entries).toEqual({});
});

it("clears the loaded document store in memory as well as persisted storage", async () => {
  setCurrentWorkspace("acme", "ws");
  const { useDocDraftStore, setDocDraft } = await import("../docs/draft-store");
  setDocDraft("ws:doc", {
    title: "Private",
    content: "A's unsaved edit",
    baseTitle: "Private",
    baseContent: "Original",
    baseRevision: 1,
    backup: { title: "Old title", content: "A's conflict backup" },
  });

  clearClientSessionData(new QueryClient());

  expect(useDocDraftStore.getState().draft.entries).toEqual({});
  expect(localStorage.getItem("doc-drafts:acme")).toBeNull();
});
