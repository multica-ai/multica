/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";
import { RUNNING, withState } from "../../governance/governance-test-mocks";
import { CompanyState } from "./company-state";

const api = vi.hoisted(() => ({
  getCompanyState: vi.fn(),
  setCompanyState: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));

function renderChip() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <CompanyState />
    </QueryClientProvider>,
  );
}

describe("CompanyState", () => {
  beforeEach(() => {
    role.value = "admin";
    api.getCompanyState.mockReset();
    api.setCompanyState.mockReset();
  });
  afterEach(cleanup);

  it("shows nothing until the state is read", () => {
    api.getCompanyState.mockReturnValue(new Promise(() => {}));
    renderChip();

    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it.each([
    ["running", "Running", "Pause"],
    ["quiet", "Quiet", "Pause"],
    ["paused", "Paused", "Resume"],
    ["unknown", "Unknown", "Pause"],
  ] as const)("chip for %s says %s and offers %s", async (state, label, action) => {
    api.getCompanyState.mockResolvedValue(withState(state));
    renderChip();

    const chip = await screen.findByRole("status");
    expect(chip.textContent).toBe(label);
    expect(chip.getAttribute("data-company-state")).toBe(state);
    expect(screen.getByRole("button", { name: action })).toBeTruthy();
  });

  it("pauses a running company and waits for the server", async () => {
    api.getCompanyState.mockResolvedValueOnce(RUNNING).mockResolvedValue(withState("paused"));
    api.setCompanyState.mockResolvedValue(withState("paused"));
    renderChip();

    fireEvent.click(await screen.findByRole("button", { name: "Pause" }));

    await waitFor(() => expect(api.setCompanyState).toHaveBeenCalledWith({ state: "paused" }));
    expect((await screen.findByRole("status")).textContent).toBe("Paused");
  });

  it("resumes a paused company", async () => {
    api.getCompanyState.mockResolvedValueOnce(withState("paused")).mockResolvedValue(RUNNING);
    api.setCompanyState.mockResolvedValue(RUNNING);
    renderChip();

    fireEvent.click(await screen.findByRole("button", { name: "Resume" }));

    await waitFor(() => expect(api.setCompanyState).toHaveBeenCalledWith({ state: "running" }));
  });

  it("shows the state but no control to a member", async () => {
    role.value = "member";
    api.getCompanyState.mockResolvedValue(withState("paused"));
    renderChip();

    expect((await screen.findByRole("status")).textContent).toBe("Paused");
    expect(screen.queryByRole("button")).toBeNull();
  });
});
