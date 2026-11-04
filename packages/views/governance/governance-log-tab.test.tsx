/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { GovernanceLogTab } from "./governance-log-tab";
import { event } from "./governance-test-mocks";

const api = vi.hoisted(() => ({
  listGovernanceEvents: vi.fn(),
  exportGovernanceEventsCsv: vi.fn(),
}));
const nav = vi.hoisted(() => ({
  pathname: "/acme/governance",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
  push: vi.fn(),
}));
const save = vi.hoisted(() => ({ saveBlob: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (type: string, id: string) => `${type} ${id}` }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
  useNavigation: () => nav,
}));
vi.mock("./save-blob", () => ({ saveBlob: save.saveBlob }));

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceLogTab />
    </QueryClientProvider>,
  );
}

describe("GovernanceLogTab", () => {
  beforeEach(() => {
    nav.searchParams = new URLSearchParams("tab=log");
    nav.replace.mockReset();
    api.listGovernanceEvents.mockReset();
    api.exportGovernanceEventsCsv.mockReset();
    save.saveBlob.mockReset();
  });
  afterEach(cleanup);

  it("lists events with kind, actor, subject, summary and time", async () => {
    api.listGovernanceEvents.mockResolvedValue({
      events: [
        event("ev-1", { kind: "budget_hold", actor_type: "agent", actor_id: "ag-7", subject_type: "squad", summary: "Held a routine run" }),
        event("ev-2", { kind: "unknown", actor_type: "policy", summary: "Something new" }),
      ],
      next_cursor: null,
    });
    renderTab();

    const row = (await screen.findByText("Held a routine run")).closest("tr");
    expect(row).toBeTruthy();
    expect(within(row!).getByText("Run held by budget")).toBeTruthy();
    expect(within(row!).getByText("agent ag-7")).toBeTruthy();
    expect(within(row!).getByText("Squad")).toBeTruthy();
    expect(screen.getByText("Unknown event")).toBeTruthy();
    expect(screen.getByText("Policy")).toBeTruthy();
  });

  it("reads its filters from the URL and sends them with the request", async () => {
    nav.searchParams = new URLSearchParams("tab=log&kind=budget_hold&actor_type=agent");
    api.listGovernanceEvents.mockResolvedValue({ events: [event("ev-1")], next_cursor: null });
    renderTab();

    await waitFor(() =>
      expect(api.listGovernanceEvents).toHaveBeenCalledWith(
        { kind: "budget_hold", actor_type: "agent" },
        { cursor: null },
      ),
    );
  });

  it("loads the next page with the cursor", async () => {
    api.listGovernanceEvents
      .mockResolvedValueOnce({ events: [event("ev-1", { summary: "First page" })], next_cursor: "c-2" })
      .mockResolvedValueOnce({ events: [event("ev-2", { summary: "Second page" })], next_cursor: null });
    renderTab();

    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));

    expect(await screen.findByText("Second page")).toBeTruthy();
    expect(screen.getByText("First page")).toBeTruthy();
    expect(api.listGovernanceEvents).toHaveBeenLastCalledWith({}, { cursor: "c-2" });
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("says so when no event matches", async () => {
    api.listGovernanceEvents.mockResolvedValue({ events: [], next_cursor: null });
    renderTab();

    expect(await screen.findByText("No governance events match these filters.")).toBeTruthy();
  });

  it("shows an error when the log cannot be read", async () => {
    api.listGovernanceEvents.mockRejectedValue(new Error("boom"));
    renderTab();

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe("Could not load the governance log.");
  });

  it("exports the filtered log as a CSV file", async () => {
    nav.searchParams = new URLSearchParams("tab=log&kind=budget_hold");
    api.listGovernanceEvents.mockResolvedValue({ events: [event("ev-1")], next_cursor: null });
    const blob = new Blob(["id\n"], { type: "text/csv" });
    api.exportGovernanceEventsCsv.mockResolvedValue(blob);
    renderTab();

    fireEvent.click(await screen.findByRole("button", { name: "Export CSV" }));

    await waitFor(() => expect(save.saveBlob).toHaveBeenCalledTimes(1));
    expect(api.exportGovernanceEventsCsv).toHaveBeenCalledWith({ kind: "budget_hold" });
    const [savedBlob, filename] = save.saveBlob.mock.calls[0] as [Blob, string];
    expect(savedBlob).toBe(blob);
    expect(filename).toMatch(/^governance-log-\d{4}-\d{2}-\d{2}\.csv$/);
  });
});
