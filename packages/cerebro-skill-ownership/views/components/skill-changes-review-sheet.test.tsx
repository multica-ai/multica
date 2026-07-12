// @vitest-environment jsdom

// FIR-2742 — drives the cross-skill review sheet: scope toggle (mine/all),
// sort, opening a row into the diff, and approving through the shared endpoint.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";

const mockReview = vi.hoisted(() => vi.fn());
const mockGetSkill = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    reviewSkillChangeRequest: (...a: unknown[]) => mockReview(...a),
    getSkill: (...a: unknown[]) => mockGetSkill(...a),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { SkillChangesReviewSheet } from "./skill-changes-review-sheet";
import type { EnrichedSkillChange } from "../../core/select-changes";

const change = (over: Partial<EnrichedSkillChange>): EnrichedSkillChange =>
  ({
    id: "cr-a",
    skill_id: "s1",
    title: "Tighten alpha",
    description: "",
    base_version: "1.0.0",
    proposed_version: "1.1.0",
    proposed_content: "new body",
    proposed_files: [],
    status: "pending",
    proposed_by: "u-2",
    reviewed_by: null,
    reviewed_at: null,
    review_comment: "",
    created_at: "2026-02-01T00:00:00Z",
    updated_at: "2026-02-01T00:00:00Z",
    skill_name: "Alpha",
    skill_owner_id: "me",
    current_version: "1.0.0",
    mine: true,
    ...over,
  }) as EnrichedSkillChange;

const members = [{ user_id: "u-2", name: "Bob Proposer" }] as never;

function renderSheet(changes: EnrichedSkillChange[], hasMine = true) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <SkillChangesReviewSheet
        open
        onOpenChange={() => {}}
        changes={changes}
        members={members}
        hasMine={hasMine}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

beforeEach(() => {
  mockReview.mockReset().mockResolvedValue({});
  mockGetSkill.mockReset().mockResolvedValue({ id: "s1", content: "old body" });
  // useIsMobile reads matchMedia.
  window.matchMedia = vi.fn().mockImplementation((q: string) => ({
    matches: false,
    media: q,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia;
});

describe("SkillChangesReviewSheet", () => {
  it("defaults to my skills and hides changes on skills I don't own", () => {
    renderSheet([
      change({ id: "cr-a", skill_name: "Alpha", mine: true }),
      change({ id: "cr-b", skill_name: "Beta", title: "Fix beta", mine: false }),
    ]);
    expect(screen.getByText("Tighten alpha")).toBeTruthy();
    expect(screen.queryByText("Fix beta")).toBeNull();
  });

  it("switching to All skills reveals other owners' changes", () => {
    renderSheet([
      change({ id: "cr-a", mine: true }),
      change({ id: "cr-b", title: "Fix beta", mine: false }),
    ]);
    fireEvent.click(screen.getByText("All skills"));
    expect(screen.getByText("Fix beta")).toBeTruthy();
  });

  it("sorts newest-first by default and flips to oldest", () => {
    renderSheet([
      change({ id: "old", title: "Older change", created_at: "2026-01-01T00:00:00Z" }),
      change({ id: "new", title: "Newer change", created_at: "2026-03-01T00:00:00Z" }),
    ]);
    const titles = () =>
      screen.getAllByText(/change$/).map((n) => n.textContent);
    expect(titles()[0]).toBe("Newer change");
    fireEvent.click(screen.getByText("Oldest"));
    expect(titles()[0]).toBe("Older change");
  });

  it("opens a row into the diff and approves through the shared endpoint", async () => {
    renderSheet([change({ id: "cr-a", skill_id: "s1" })]);
    fireEvent.click(screen.getByText("Tighten alpha"));

    expect(screen.getByText("Review change")).toBeTruthy();
    await waitFor(() => expect(mockGetSkill).toHaveBeenCalledWith("s1"));

    fireEvent.click(screen.getByText("Approve"));
    await waitFor(() =>
      expect(mockReview).toHaveBeenCalledWith("cr-a", { action: "approve" }),
    );
  });

  it("lets the reviewer edit the proposed content and sends the edit on approve", async () => {
    renderSheet([change({ id: "cr-a", skill_id: "s1", proposed_content: "new body" })]);
    fireEvent.click(screen.getByText("Tighten alpha"));
    await waitFor(() => expect(mockGetSkill).toHaveBeenCalledWith("s1"));

    fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
    const textarea = await screen.findByPlaceholderText("Edit the proposed content…");
    fireEvent.change(textarea, { target: { value: "new body, tightened further" } });

    fireEvent.click(screen.getByText("Approve"));
    await waitFor(() =>
      expect(mockReview).toHaveBeenCalledWith("cr-a", {
        action: "approve",
        edited_content: "new body, tightened further",
      }),
    );
  });

  it("does not send an edit when the reviewer opens edit mode but leaves the text unchanged", async () => {
    renderSheet([change({ id: "cr-a", skill_id: "s1", proposed_content: "new body" })]);
    fireEvent.click(screen.getByText("Tighten alpha"));
    await waitFor(() => expect(mockGetSkill).toHaveBeenCalledWith("s1"));

    fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));

    fireEvent.click(screen.getByText("Approve"));
    await waitFor(() =>
      expect(mockReview).toHaveBeenCalledWith("cr-a", { action: "approve" }),
    );
  });
});
