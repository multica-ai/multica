/**
 * @vitest-environment jsdom
 */
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ReviewCommentSidebar } from "./review-comment-sidebar";
import type { ReviewAsset } from "@multica/core/types";

// Mock the zustand store and core hooks
vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: vi.fn() })
  }
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: vi.fn(() => ({ user: { id: "user1" } }))
}));

vi.mock("@multica/core/workspace", () => ({
  useActorName: () => ({ getActorName: () => "Test User" })
}));

vi.mock("@multica/core/reviews", () => ({
  useCreateReviewComment: () => ({ mutate: vi.fn(), isPending: false }),
  useResolveReviewComment: () => ({ mutate: vi.fn() }),
  useUnresolveReviewComment: () => ({ mutate: vi.fn() }),
  useDeleteReviewComment: () => ({ mutate: vi.fn() }),
  useUpdateReviewComment: () => ({ mutate: vi.fn() }),
}));

// Mock the ContentEditor to simplify rendering
vi.mock("../editor", () => ({
  ContentEditor: vi.fn(() => <div data-testid="mock-content-editor" />)
}));

describe("ReviewCommentSidebar", () => {
  it("renders the sidebar header", () => {
    const asset = { id: "1", asset_type: "video", issue_id: "1" } as ReviewAsset;
    
    render(
      <ReviewCommentSidebar
        workspaceId="ws-1"
        asset={asset}
        currentTime={0}
        onSeek={vi.fn()}
        onDrawStart={vi.fn()}
        getCanvasShapes={vi.fn(() => [])}
        clearCanvasShapes={vi.fn()}
      />
    );
    
    expect(screen.getByText("Review Comments")).toBeInTheDocument();
    expect(screen.getByTestId("mock-content-editor")).toBeInTheDocument();
  });

  it("shows loading state when isLoading is true", () => {
    const asset = { id: "1", asset_type: "video", issue_id: "1" } as ReviewAsset;
    
    render(
      <ReviewCommentSidebar
        workspaceId="ws-1"
        asset={asset}
        currentTime={0}
        onSeek={vi.fn()}
        onDrawStart={vi.fn()}
        getCanvasShapes={vi.fn(() => [])}
        clearCanvasShapes={vi.fn()}
        isLoading={true}
      />
    );
    
    expect(screen.getByText("Loading comments...")).toBeInTheDocument();
  });

  it("renders comments properly", () => {
    const asset = { id: "1", asset_type: "video", issue_id: "1" } as ReviewAsset;
    const comments = [
      { id: "c1", content: "Test comment", author_id: "user2", resolved: false, start_time: 1, end_time: 2 }
    ] as any[];
    
    render(
      <ReviewCommentSidebar
        workspaceId="ws-1"
        asset={asset}
        currentTime={0}
        onSeek={vi.fn()}
        onDrawStart={vi.fn()}
        getCanvasShapes={vi.fn(() => [])}
        clearCanvasShapes={vi.fn()}
        comments={comments}
      />
    );
    
    expect(screen.getByText("Test comment")).toBeInTheDocument();
  });

  it("toggles the end time duration when clicking the clock button", () => {
    const asset = { id: "1", asset_type: "video", issue_id: "1" } as ReviewAsset;
    
    render(
      <ReviewCommentSidebar
        workspaceId="ws-1"
        asset={asset}
        currentTime={5}
        onSeek={vi.fn()}
        onDrawStart={vi.fn()}
        getCanvasShapes={vi.fn(() => [])}
        clearCanvasShapes={vi.fn()}
      />
    );
    
    const rangeButton = screen.getByTitle("Set end time (duration)");
    expect(rangeButton).toBeInTheDocument();
    
    fireEvent.click(rangeButton);
    
    // Now it should have removed the end time or toggled it
    // The button title changes to 'Remove end time'
    expect(screen.getByTitle("Remove end time")).toBeInTheDocument();
  });
});
