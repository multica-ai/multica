/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Proposal } from "@multica/core/company";
import { renderWithI18n } from "../../test/i18n";
import { OrgChangeSummary } from "./company-org-change-summary";

const api = vi.hoisted(() => ({ getAgent: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

// Spec 4.9: an organisation change carries instruction text and a summary that
// an agent or an imported document wrote. The decision page shows them as text,
// so markup in them never becomes an element.
const HOSTILE = '<img src=x onerror="window.__orgChangeXss = true"><script>window.__orgChangeXss = true</script>';

function instructionChange(): Proposal {
  return {
    id: "prop-1",
    state: "pending",
    payload: {
      type: "instruction_change",
      agent_id: "ag-1",
      instructions: HOSTILE,
      diff_summary: HOSTILE,
      rationale: HOSTILE,
    },
  } as unknown as Proposal;
}

describe("OrgChangeSummary with markup in the change", () => {
  afterEach(() => {
    cleanup();
    api.getAgent.mockReset();
  });

  it("shows instruction text and the summary as text, never as elements", async () => {
    api.getAgent.mockResolvedValue({ id: "ag-1", instructions: "Be concise." });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OrgChangeSummary proposal={instructionChange()} />
      </QueryClientProvider>,
    );

    // The diff is drawn once the current agent loads; its added line carries the markup as text.
    await waitFor(() => expect(document.querySelector("[data-diff-kind='added']")).not.toBeNull());
    expect(screen.getAllByText(HOSTILE).length).toBeGreaterThan(0);
    expect(document.querySelector("img")).toBeNull();
    expect(document.querySelector("script")).toBeNull();
    expect((window as unknown as { __orgChangeXss?: boolean }).__orgChangeXss).toBeUndefined();
  });
});

describe("OrgChangeSummary for the rollback types", () => {
  afterEach(cleanup);

  it.each([
    ["unretire", "Unretire"],
    ["unmerge", "Unmerge squads"],
  ])("names a %s change by its type rather than the generic kind", (type, heading) => {
    const proposal = {
      id: "prop-2",
      state: "pending",
      payload: { type, agent_id: "", squad_id: "", rationale: "Probation found the earlier change worse." },
    } as unknown as Proposal;
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OrgChangeSummary proposal={proposal} />
      </QueryClientProvider>,
    );

    expect(screen.getByRole("heading", { name: heading })).toBeTruthy();
  });
});
