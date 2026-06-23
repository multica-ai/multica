// @vitest-environment jsdom

// FIR-1957 — change requests render as compact lines; clicking a line opens a
// single modal containing the diff, an inline comment, and approve/reject.

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

const mockReview = vi.hoisted(() => vi.fn());
const mockList = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listSkillChangeRequests: (...args: unknown[]) => mockList(...args),
    reviewSkillChangeRequest: (...args: unknown[]) => mockReview(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { SkillChangeRequestQueue } from "@multica/cerebro-skill-ownership/views";

const skill = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "My Skill",
  description: "",
  content: "line one\nline two\nline three",
  current_version: "1.0.0",
  config: {},
  files: [],
  owner_id: "user-1",
  approver_ids: [],
  created_by: "user-1",
  created_at: "2026-06-23T00:00:00Z",
  updated_at: "2026-06-23T00:00:00Z",
} as never;

const members = [
  { user_id: "user-2", name: "Proposer Person" },
] as never;

const pendingRequest = {
  id: "cr-1",
  skill_id: "skill-1",
  title: "Tighten the rules",
  description: "Adds an extra line",
  base_version: "1.0.0",
  proposed_version: "1.1.0",
  proposed_content: "line one\nline two\nline three\nline four",
  proposed_files: [],
  status: "pending",
  proposed_by: "user-2",
  reviewed_by: null,
  reviewed_at: null,
  review_comment: "",
  created_at: "2026-06-23T00:00:00Z",
  updated_at: "2026-06-23T00:00:00Z",
};

function Wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function renderQueue() {
  return render(
    <Wrapper>
      <SkillChangeRequestQueue skill={skill} wsId="ws-1" members={members} />
    </Wrapper>,
  );
}

describe("SkillChangeRequestQueue", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockList.mockResolvedValue([pendingRequest]);
    mockReview.mockResolvedValue({ ...pendingRequest, status: "approved" });
  });

  it("renders each change request as a line without an inline diff", async () => {
    renderQueue();

    expect(
      await screen.findByText("Tighten the rules", {}, { timeout: 30000 }),
    ).toBeTruthy();
    // The diff is NOT shown inline — only inside the modal.
    expect(screen.queryByText("current (1.0.0)")).toBeNull();
    expect(screen.queryByText("line four")).toBeNull();
  });

  it("opens a modal with the diff, a comment box, and approve/reject when a line is clicked", async () => {
    renderQueue();

    const row = await screen.findByText("Tighten the rules", {}, { timeout: 30000 });
    fireEvent.click(row);

    // Modal now shows the diff (added line) and version labels.
    expect(await screen.findByText("line four")).toBeTruthy();
    expect(screen.getByText(/current \(1\.0\.0\)/)).toBeTruthy();
    expect(
      screen.getByPlaceholderText("Optional comment for the proposer…"),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: /approve/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /reject/i })).toBeTruthy();
  });

  it("submits an approval with the inline comment", async () => {
    renderQueue();

    const row = await screen.findByText("Tighten the rules", {}, { timeout: 30000 });
    fireEvent.click(row);

    const textarea = await screen.findByPlaceholderText(
      "Optional comment for the proposer…",
    );
    fireEvent.change(textarea, { target: { value: "Looks good" } });
    fireEvent.click(screen.getByRole("button", { name: /approve/i }));

    await waitFor(() =>
      expect(mockReview).toHaveBeenCalledWith("cr-1", {
        action: "approve",
        comment: "Looks good",
      }),
    );
  });
});
