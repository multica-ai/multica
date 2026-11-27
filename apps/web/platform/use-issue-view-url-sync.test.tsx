import { StrictMode, type ReactNode } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { useIssueViewUrlSync } from "./use-issue-view-url-sync";

vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(window.location.search),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  useActiveIssueViewStore.setState({ active: {} });
  window.history.replaceState(null, "", "/");
});

it("adopts the saved view without rewriting the deep link and keeps the selected run when the view changes", () => {
  const scope = { scope_type: "project", scope_id: "project-1" } as const;
  const key = issueViewContainerKey("workspace-1", scope);
  const deepLink = "/acme/projects/project-1?tab=issues&view=custom&trajectory=1&target_kind=run&target_id=run-1#section";
  window.history.replaceState({ __NA: true }, "", deepLink);
  const replaceState = vi.spyOn(window.history, "replaceState");
  const historyLength = window.history.length;

  renderHook(() => useIssueViewUrlSync(scope), {
    wrapper: ({ children }: { children: ReactNode }) => <StrictMode>{children}</StrictMode>,
  });

  expect(useActiveIssueViewStore.getState().active[key]).toBe("custom");
  expect(replaceState).not.toHaveBeenCalled();

  act(() => useActiveIssueViewStore.getState().setActive(key, "other"));

  expect(replaceState).toHaveBeenCalledWith(null, "", expect.stringContaining("view=other"));
  expect(window.location.pathname + window.location.search + window.location.hash)
    .toBe(deepLink.replace("view=custom", "view=other"));
  expect(window.history.length).toBe(historyLength);

  act(() => useActiveIssueViewStore.getState().setActive(key, null));

  expect(window.location.pathname + window.location.search + window.location.hash)
    .toBe(deepLink.replace("view=custom&", ""));
});
