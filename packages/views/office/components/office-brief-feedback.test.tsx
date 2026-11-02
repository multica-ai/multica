/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { BriefFeedback, BriefFeedbackSummary } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { OfficeBriefFeedback } from "./office-brief-feedback";

const mocks = vi.hoisted(() => ({
  mine: vi.fn(),
  submit: vi.fn(),
  summary: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/api", () => ({
  api: {
    getMyBriefFeedback: mocks.mine,
    submitBriefFeedback: mocks.submit,
    getBriefFeedbackSummary: mocks.summary,
  },
}));

const SAVED: BriefFeedback = {
  id: "f1",
  brief_id: "b1",
  rating: "not_useful",
  tags: ["wrong"],
  note: "Lead with decisions",
  created_at: "2026-11-02T08:00:00Z",
  updated_at: "2026-11-02T09:00:00Z",
};

const SUMMARY: BriefFeedbackSummary = { days: 14, ratings: 4, useful: 3, usefulness: 0.75 };

function renderFeedback(canManage = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <OfficeBriefFeedback briefId="b1" canManage={canManage} />
    </QueryClientProvider>,
  );
}

const pressed = (name: string) => screen.findByRole("button", { name }).then((el) => el.getAttribute("aria-pressed"));

beforeEach(() => {
  mocks.mine.mockResolvedValue(null);
  mocks.submit.mockImplementation(async (_briefId: string, data: object) => ({ ...SAVED, ...data }));
  mocks.summary.mockResolvedValue(SUMMARY);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("OfficeBriefFeedback", () => {
  it("shows the caller's existing rating, tags and note when the brief opens", async () => {
    mocks.mine.mockResolvedValue(SAVED);
    renderFeedback();

    expect(await pressed("Not useful")).toBe("true");
    expect(await pressed("Useful")).toBe("false");
    expect(await pressed("Wrong")).toBe("true");
    expect(await pressed("Too long")).toBe("false");
    expect((screen.getByLabelText("Note (optional)") as HTMLTextAreaElement).value).toBe("Lead with decisions");
  });

  it("sends the rating, the chosen tags and the trimmed note", async () => {
    renderFeedback();

    fireEvent.click(await screen.findByRole("button", { name: "Not useful" }));
    fireEvent.click(screen.getByRole("button", { name: "Too long" }));
    fireEvent.click(screen.getByRole("button", { name: "Not for me" }));
    fireEvent.change(screen.getByLabelText("Note (optional)"), { target: { value: "  Skip the floor report  " } });
    fireEvent.click(screen.getByRole("button", { name: "Send feedback" }));

    await waitFor(() => expect(mocks.submit).toHaveBeenCalledTimes(1));
    expect(mocks.submit).toHaveBeenCalledWith("b1", {
      rating: "not_useful",
      tags: ["too_long", "not_for_me"],
      note: "Skip the floor report",
    });
  });

  it("offers no tags for a useful rating and sends none", async () => {
    renderFeedback();

    fireEvent.click(await screen.findByRole("button", { name: "Useful" }));
    expect(screen.queryByRole("button", { name: "Too long" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Send feedback" }));

    await waitFor(() => expect(mocks.submit).toHaveBeenCalledTimes(1));
    expect(mocks.submit).toHaveBeenCalledWith("b1", { rating: "useful", tags: [], note: "" });
  });

  it("keeps sending disabled until a rating is chosen", async () => {
    renderFeedback();

    const send = await screen.findByRole("button", { name: "Send feedback" });
    expect((send as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows the usefulness figure to an owner or admin", async () => {
    renderFeedback(true);

    expect(await screen.findByText("75% useful over the last 14 days")).toBeTruthy();
    expect(mocks.summary).toHaveBeenCalledTimes(1);
  });

  it("does not ask for or show the usefulness figure to a member", async () => {
    renderFeedback(false);

    await screen.findByRole("button", { name: "Useful" });
    expect(screen.queryByText(/useful over the last/)).toBeNull();
    expect(mocks.summary).not.toHaveBeenCalled();
  });

  it("hides the usefulness figure when the server refuses it with a 403", async () => {
    mocks.summary.mockRejectedValue(Object.assign(new Error("forbidden"), { status: 403 }));
    renderFeedback(true);

    await screen.findByRole("button", { name: "Useful" });
    await waitFor(() => expect(mocks.summary).toHaveBeenCalled());
    expect(screen.queryByText(/useful over the last/)).toBeNull();
  });

  it("hides the usefulness figure while the window has no ratings", async () => {
    mocks.summary.mockResolvedValue({ days: 14, ratings: 0, useful: 0, usefulness: null });
    renderFeedback(true);

    await screen.findByRole("button", { name: "Useful" });
    await waitFor(() => expect(mocks.summary).toHaveBeenCalled());
    expect(screen.queryByText(/useful over the last/)).toBeNull();
  });
});
