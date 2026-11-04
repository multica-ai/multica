/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { briefKeys, useCompanyBriefStore, type Brief } from "@multica/core/company";
import { renderWithI18n } from "../../test/i18n";
import { CompanyBriefButton } from "./company-brief-button";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("../lib/use-company-admin", () => ({ useCompanyAdmin: () => true }));

// The dialog has its own tests; here it only reports that it is open.
vi.mock("./company-brief-dialog", () => ({
  CompanyBriefDialog: ({ open }: { open: boolean }) => (open ? <div role="dialog" aria-label="Brief" /> : null),
}));

function brief(id: string): Brief {
  return {
    id,
    workspace_id: "ws-1",
    kind: "daily",
    period_start: "",
    period_end: "",
    title: `Brief ${id}`,
    body: "",
    proposal_ids: [],
    created_by_type: "agent",
    created_by_id: "sec-1",
    created_at: "2026-11-02T08:00:00Z",
  };
}

describe("CompanyBriefButton", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    useCompanyBriefStore.setState({ lastSeenBriefId: null });
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(briefKeys.listFiltered("ws-1"), [brief("b2"), brief("b1")]);
  });
  afterEach(() => {
    cleanup();
  });

  function setup() {
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <CompanyBriefButton />
      </QueryClientProvider>,
    );
  }

  it("shows the unread dot when the newest brief was not the last one opened", () => {
    useCompanyBriefStore.setState({ lastSeenBriefId: "b1" });
    setup();
    expect(screen.getByText("New brief")).toBeTruthy();
  });

  it("shows no dot once the newest brief is the one last opened", () => {
    useCompanyBriefStore.setState({ lastSeenBriefId: "b2" });
    setup();
    expect(screen.queryByText("New brief")).toBeNull();
  });

  it("shows the dot for a first visit and clears it when the dialog opens", () => {
    setup();
    expect(screen.getByText("New brief")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Brief/ }));
    expect(useCompanyBriefStore.getState().lastSeenBriefId).toBe("b2");
    expect(screen.getByRole("dialog", { name: "Brief" })).toBeTruthy();
    expect(screen.queryByText("New brief")).toBeNull();
  });

  it("has no dot when there is no brief at all", () => {
    queryClient.setQueryData(briefKeys.listFiltered("ws-1"), []);
    setup();
    expect(screen.queryByText("New brief")).toBeNull();
  });
});
