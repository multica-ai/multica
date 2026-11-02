/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { charterKeys, emptySquadCharter, type SquadCharter } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { OfficeCharterTab } from "./office-charter-tab";

const api = vi.hoisted(() => ({
  getSquadCharter: vi.fn(),
  updateSquadCharter: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const CHARTER: SquadCharter = {
  ...emptySquadCharter("sq"),
  mission: "Find the market gaps",
  responsibilities: [{ key: "r1", text: "Track competitors" }],
  outputs: [{ key: "o1", kind: "kb_document", kb_key: "market", cadence_hint: "weekly" }],
  boundaries: "No pricing changes",
  escalation: "Ask the boss about spend",
  intake: "",
  response_hours: 24,
  operating_review_enabled: true,
  revision: 4,
  updated_by_type: "member",
  updated_by_id: "u1",
  updated_at: "2026-11-01T00:00:00Z",
};

describe("OfficeCharterTab", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    api.getSquadCharter.mockResolvedValue(CHARTER);
  });

  afterEach(() => {
    cleanup();
  });

  function setup({ charter = CHARTER, canEdit = false }: { charter?: SquadCharter; canEdit?: boolean } = {}) {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(charterKeys.charter("ws-1", "sq"), charter);
    return renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OfficeCharterTab squadId="sq" canEdit={canEdit} />
      </QueryClientProvider>,
    );
  }

  describe("read view", () => {
    it("shows the charter to a member, with no edit control", () => {
      setup({ canEdit: false });

      expect(screen.getByText("Find the market gaps")).toBeTruthy();
      expect(screen.getByText("Track competitors")).toBeTruthy();
      expect(screen.getByText("Knowledge document · market · weekly")).toBeTruthy();
      expect(screen.getByText("No pricing changes")).toBeTruthy();
      expect(screen.getByText("Responds within 24 hours")).toBeTruthy();
      expect(screen.getByText("On")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    });

    it("marks a field nobody wrote as not set", () => {
      setup({ canEdit: false });

      const intake = screen.getByRole("heading", { name: "Intake" }).parentElement!;
      expect(within(intake).getByText("Not set")).toBeTruthy();
    });

    it("tells a member that a squad has no charter yet", () => {
      setup({ charter: emptySquadCharter("sq"), canEdit: false });

      expect(screen.getByText("No charter yet.")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Save charter" })).toBeNull();
    });
  });

  describe("edit view", () => {
    it("opens the editor with the saved values for an admin or the creator", () => {
      setup({ canEdit: true });

      fireEvent.click(screen.getByRole("button", { name: "Edit" }));

      expect((screen.getByLabelText("Mission") as HTMLTextAreaElement).value).toBe("Find the market gaps");
      expect((screen.getByLabelText("Response time (hours)") as HTMLInputElement).value).toBe("24");
      expect((screen.getByRole("switch") as HTMLElement).getAttribute("aria-checked")).toBe("true");
    });

    it("saves at the revision it opened, with the edited fields", async () => {
      const saved = { ...CHARTER, mission: "Find gaps fast", revision: 5 };
      api.updateSquadCharter.mockResolvedValue(saved);
      api.getSquadCharter.mockResolvedValue(saved);
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));

      fireEvent.change(screen.getByLabelText("Mission"), { target: { value: "  Find gaps fast  " } });
      fireEvent.click(screen.getByRole("button", { name: "Save charter" }));

      await waitFor(() => expect(api.updateSquadCharter).toHaveBeenCalledTimes(1));
      const [squadId, request] = api.updateSquadCharter.mock.calls[0]!;
      expect(squadId).toBe("sq");
      expect(request).toMatchObject({
        revision: 4,
        mission: "Find gaps fast",
        response_hours: 24,
        operating_review_enabled: true,
        responsibilities: [{ key: "r1", text: "Track competitors" }],
      });
      await waitFor(() => expect(screen.queryByRole("button", { name: "Save charter" })).toBeNull());
      expect(queryClient.getQueryData(charterKeys.charter("ws-1", "sq"))).toMatchObject({ revision: 5 });
    });

    it("sends the operating review switch when the boss turns it off", async () => {
      api.updateSquadCharter.mockResolvedValue({ ...CHARTER, operating_review_enabled: false, revision: 5 });
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));

      fireEvent.click(screen.getByRole("switch"));
      fireEvent.click(screen.getByRole("button", { name: "Save charter" }));

      await waitFor(() => expect(api.updateSquadCharter).toHaveBeenCalled());
      expect(api.updateSquadCharter.mock.calls[0]![1]).toMatchObject({ operating_review_enabled: false });
    });

    it("keeps the save disabled for response hours outside 1 to 720", () => {
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));

      fireEvent.change(screen.getByLabelText("Response time (hours)"), { target: { value: "0" } });

      expect(screen.getByText("Enter a whole number from 1 to 720.")).toBeTruthy();
      expect((screen.getByRole("button", { name: "Save charter" }) as HTMLButtonElement).disabled).toBe(true);
    });

    it("drops a responsibility left blank before saving", async () => {
      api.updateSquadCharter.mockResolvedValue({ ...CHARTER, revision: 5 });
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));

      fireEvent.click(screen.getByRole("button", { name: "Add responsibility" }));
      fireEvent.click(screen.getByRole("button", { name: "Save charter" }));

      await waitFor(() => expect(api.updateSquadCharter).toHaveBeenCalled());
      expect(api.updateSquadCharter.mock.calls[0]![1].responsibilities).toEqual([
        { key: "r1", text: "Track competitors" },
      ]);
    });

    it("opens straight into the editor for a squad with no charter", () => {
      setup({ charter: emptySquadCharter("sq"), canEdit: true });

      expect(screen.getByLabelText("Mission")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
    });
  });

  describe("revision conflict", () => {
    function conflict(revision: number) {
      return new ApiError("the charter changed since you read it", 409, "Conflict", {
        code: "charter_revision_conflict",
        revision,
      });
    }

    it("says the save was refused and offers a reload, keeping the edits out of the charter", async () => {
      api.updateSquadCharter.mockRejectedValue(conflict(7));
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));
      fireEvent.change(screen.getByLabelText("Mission"), { target: { value: "My edit" } });

      fireEvent.click(screen.getByRole("button", { name: "Save charter" }));

      expect(await screen.findByText(/Someone changed the charter while you were editing/)).toBeTruthy();
      expect(queryClient.getQueryData(charterKeys.charter("ws-1", "sq"))).toMatchObject({ revision: 4 });
    });

    it("reloads the server's charter when the boss asks for it", async () => {
      api.updateSquadCharter.mockRejectedValue(conflict(7));
      api.getSquadCharter.mockResolvedValue({ ...CHARTER, mission: "Changed by someone else", revision: 7 });
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));
      fireEvent.change(screen.getByLabelText("Mission"), { target: { value: "My edit" } });
      fireEvent.click(screen.getByRole("button", { name: "Save charter" }));
      await screen.findByText(/Someone changed the charter/);

      fireEvent.click(screen.getByRole("button", { name: "Reload" }));

      await waitFor(() =>
        expect((screen.getByLabelText("Mission") as HTMLTextAreaElement).value).toBe("Changed by someone else"),
      );
      expect(screen.queryByText(/Someone changed the charter/)).toBeNull();
    });

    it("reports other save failures as a toast-level error, not a conflict", async () => {
      api.updateSquadCharter.mockRejectedValue(
        new ApiError("output kind must be issue, proposal, kb_document or report", 400, "Bad Request", {}),
      );
      setup({ canEdit: true });
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));

      fireEvent.click(screen.getByRole("button", { name: "Save charter" }));

      await waitFor(() => expect(api.updateSquadCharter).toHaveBeenCalled());
      expect(screen.queryByText(/Someone changed the charter/)).toBeNull();
    });
  });
});
