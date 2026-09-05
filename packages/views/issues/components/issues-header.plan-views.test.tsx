// @vitest-environment jsdom

/**
 * The server-backed `project_plans` flag gates three real menu entries in the
 * view-mode switcher. `allowPlanViews` in use-issue-surface-controller is
 * covered at the controller level; this file is the component-level assertion
 * that was missing — the flag must
 * actually hide and reveal the rendered `DropdownMenuRadioItem`s, not just
 * flip a boolean the controller carries.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { viewStoreSlice, type IssueViewState } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { renderWithI18n } from "../../test/i18n";
import { IssueDisplayControls } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function renderControls(allowPlanViews: boolean) {
  setApiInstance({
    listProperties: async () => ({ properties: [] }),
  } as unknown as ApiClient);

  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const store = createStore<IssueViewState>()(viewStoreSlice);

  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <IssueDisplayControls scopedIssues={[]} allowPlanViews={allowPlanViews} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
}

function openViewMenu() {
  fireEvent.click(screen.getByRole("button", { name: /board/i }));
}

describe("IssueDisplayControls — plan view menu entries", () => {
  afterEach(cleanup);

  it("hides Plan Document/Pipeline/Coverage when the flag is off", () => {
    renderControls(false);
    openViewMenu();

    expect(screen.queryByRole("menuitemradio", { name: /document/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: /pipeline/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: /coverage/i })).not.toBeInTheDocument();
  });

  it("reveals all three Plan entries when the flag is on", () => {
    renderControls(true);
    openViewMenu();

    expect(screen.getByRole("menuitemradio", { name: /document/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /pipeline/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /coverage/i })).toBeInTheDocument();
  });
});
