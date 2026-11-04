/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { GovernanceControlsTab } from "./governance-controls-tab";
import { RUNNING, withState } from "./governance-test-mocks";

const api = vi.hoisted(() => ({
  getCompanyState: vi.fn(),
  setCompanyState: vi.fn(),
  setQuietHours: vi.fn(),
  getDigestMode: vi.fn(),
  setDigestMode: vi.fn(),
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

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceControlsTab />
    </QueryClientProvider>,
  );
}

const segment = (name: string) => screen.getByRole("button", { name });

describe("GovernanceControlsTab", () => {
  beforeEach(() => {
    role.value = "admin";
    api.getCompanyState.mockReset();
    api.setCompanyState.mockReset();
    api.setQuietHours.mockReset();
    api.getDigestMode.mockReset();
    api.setDigestMode.mockReset();
  });
  afterEach(cleanup);

  it("shows the company state the server returned", async () => {
    api.getCompanyState.mockResolvedValue(withState("quiet"));
    renderTab();

    await waitFor(() => expect(segment("Quiet").getAttribute("aria-pressed")).toBe("true"));
    expect(segment("Running").getAttribute("aria-pressed")).toBe("false");
  });

  it("shows the boss's setting, and says when a quiet window is in effect instead", async () => {
    api.getCompanyState.mockResolvedValue(withState("quiet", { setting: "running" }));
    renderTab();

    await waitFor(() => expect(segment("Running").getAttribute("aria-pressed")).toBe("true"));
    expect(segment("Quiet").getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByText("In effect now: Quiet")).toBeTruthy();
  });

  it("changes the state only once the server has answered", async () => {
    api.getCompanyState.mockResolvedValueOnce(RUNNING).mockResolvedValue(withState("paused"));
    let answer: (value: unknown) => void = () => {};
    api.setCompanyState.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    renderTab();

    await waitFor(() => expect(segment("Running").getAttribute("aria-pressed")).toBe("true"));
    fireEvent.click(segment("Paused"));

    await waitFor(() => expect(api.setCompanyState).toHaveBeenCalledWith({ state: "paused" }));
    expect(segment("Running").getAttribute("aria-pressed")).toBe("true");

    answer(withState("paused"));
    await waitFor(() => expect(segment("Paused").getAttribute("aria-pressed")).toBe("true"));
  });

  it("confirms Pause and stop running work before sending it, and sends stop_running", async () => {
    api.getCompanyState.mockResolvedValueOnce(RUNNING).mockResolvedValue(withState("paused"));
    api.setCompanyState.mockResolvedValue(withState("paused"));
    renderTab();

    await waitFor(() => expect(segment("Running").getAttribute("aria-pressed")).toBe("true"));
    fireEvent.click(segment("Pause and stop running work"));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Running routine and delegated tasks are cancelled. Queued ones wait until you resume.")).toBeTruthy();
    expect(api.setCompanyState).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Pause and stop" }));

    await waitFor(() =>
      expect(api.setCompanyState).toHaveBeenCalledWith({ state: "paused", stop_running: true }),
    );
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("keeps the confirmation open when the stop fails", async () => {
    api.getCompanyState.mockResolvedValue(RUNNING);
    api.setCompanyState.mockRejectedValue(new Error("nope"));
    renderTab();

    await waitFor(() => expect(segment("Running").getAttribute("aria-pressed")).toBe("true"));
    fireEvent.click(segment("Pause and stop running work"));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Pause and stop" }));

    await waitFor(() => expect(api.setCompanyState).toHaveBeenCalled());
    expect(screen.getByRole("alertdialog")).toBeTruthy();
  });

  it("saves quiet hours from the form", async () => {
    api.getCompanyState.mockResolvedValueOnce(RUNNING).mockResolvedValue(RUNNING);
    api.setQuietHours.mockResolvedValue(RUNNING);
    renderTab();

    const save = await screen.findByRole("button", { name: "Save quiet hours" });
    expect((save as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("switch"));
    expect((save as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(save);

    await waitFor(() =>
      expect(api.setQuietHours).toHaveBeenCalledWith({ enabled: true, start: "22:00", end: "07:00", timezone: "UTC" }),
    );
  });

  it("lets a member read the state but not change it", async () => {
    role.value = "member";
    api.getCompanyState.mockResolvedValue(RUNNING);
    renderTab();

    await waitFor(() => expect(segment("Paused").hasAttribute("disabled")).toBe(true));
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
    expect(screen.getByText("Only owners and admins can change these settings.")).toBeTruthy();
  });
  it("shows the Secretary's digest source, with what automatic resolves to", async () => {
    api.getCompanyState.mockResolvedValue(withState("running"));
    api.getDigestMode.mockResolvedValue({ setting: "auto", active_squads: 12, resolved: "squads", auto_floor_threshold: 30 });
    renderTab();
    const automatic = await screen.findByRole("button", { name: "Automatic" });
    expect(automatic.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText(/reads floor briefs above 30 active squads/)).toBeTruthy();
  });

  it("changes the digest source only when the server answers", async () => {
    api.getCompanyState.mockResolvedValue(withState("running"));
    const automatic = { setting: "auto", active_squads: 12, resolved: "squads", auto_floor_threshold: 30 };
    const floors = { setting: "floors", active_squads: 12, resolved: "floors", auto_floor_threshold: 30 };
    // The first read is automatic; after the save the server reports floors, and
    // the refetch that follows the save reads that.
    api.getDigestMode.mockResolvedValueOnce(automatic).mockResolvedValue(floors);
    api.setDigestMode.mockResolvedValue(floors);
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Floors" }));
    await waitFor(() => expect(api.setDigestMode).toHaveBeenCalledWith("floors"));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Floors" }).getAttribute("aria-pressed")).toBe("true"),
    );
  });
});
