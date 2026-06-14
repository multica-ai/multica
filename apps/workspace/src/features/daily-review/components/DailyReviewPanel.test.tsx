import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { DailyReviewPanel } from "./DailyReviewPanel";
import type { DailyReview } from "@/shared/types";

const confirmMutate = vi.fn();
const generateMutate = vi.fn();

const draftReview: DailyReview = {
  id: "review-1",
  workspace_id: "workspace-1",
  user_id: "user-1",
  review_date: "2026-06-13",
  draft_content: "## 今日完成\n- Review focus signals",
  status: "draft",
  confirmed_at: null,
  generated_by: "manual",
  created_at: "2026-06-13T10:00:00.000Z",
  updated_at: "2026-06-13T10:00:00.000Z",
  energy_level: null,
  energy_note: null,
  recovery_need: null,
};

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("./ReviewMarkdownView", () => ({
  ReviewMarkdownView: ({ review }: { review: DailyReview }) => (
    <article>{review.draft_content}</article>
  ),
}));

vi.mock("../hooks/use-daily-review", () => ({
  useTodayReviewQuery: () => ({ data: draftReview, isLoading: false }),
  useGenerateReviewMutation: () => ({ mutate: generateMutate, isPending: false }),
  useConfirmReviewMutation: () => ({ mutate: confirmMutate, isPending: false }),
}));

describe("DailyReviewPanel energy check-in", () => {
  beforeEach(() => {
    confirmMutate.mockClear();
    generateMutate.mockClear();
  });

  it("submits optional energy fields when confirming a draft review", () => {
    render(<DailyReviewPanel />);

    fireEvent.change(screen.getByLabelText("今日精力"), {
      target: { value: "2" },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "明天需要降低负载或安排恢复" }));
    fireEvent.change(screen.getByPlaceholderText("精力备注，可选"), {
      target: { value: "Need a lighter plan" },
    });
    fireEvent.click(screen.getByRole("button", { name: /确认/i }));

    expect(confirmMutate).toHaveBeenCalledTimes(1);
    expect(confirmMutate).toHaveBeenCalledWith({
      reviewId: "review-1",
      data: {
        energy_level: 2,
        energy_note: "Need a lighter plan",
        recovery_need: true,
      },
    }, expect.any(Object));
  });
});
