/**
 * @vitest-environment jsdom
 */
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { MediaReviewLayout } from "./media-review-layout";
import type { ReviewAsset } from "@multica/core/types";

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceSlug: () => "test-ws"
}));

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@multica/core/reviews", () => ({
  useReviewAssets: () => ({ data: [], isLoading: false }),
  useReviewComments: () => ({ data: [], isLoading: false }),
  useDeleteReviewAsset: () => ({ mutate: vi.fn(), isPending: false }),
  listReviewAssetsOptions: () => ({ queryKey: ["test"], queryFn: () => [] }),
  listReviewCommentsOptions: () => ({ queryKey: ["test"], queryFn: () => [] }),
  useUpdateReviewAssetStatus: () => ({ mutate: vi.fn() })
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: any) => <>{children}</>,
  Tooltip: ({ children }: any) => <>{children}</>,
  TooltipTrigger: ({ children }: any) => <>{children}</>,
  TooltipContent: ({ children }: any) => <>{children}</>,
}));

vi.mock("./media-review-player", () => ({
  MediaReviewPlayer: vi.fn(() => <div data-testid="mock-media-player" />)
}));

vi.mock("./review-comment-sidebar", () => ({
  ReviewCommentSidebar: vi.fn(() => <div data-testid="mock-sidebar" />)
}));

describe("MediaReviewLayout", () => {
  it("renders empty state when no assets", () => {
    const queryClient = new QueryClient();
    const mockAsset = { id: "1", issue_id: "1", asset_type: "video" } as ReviewAsset;
    render(
      <QueryClientProvider client={queryClient}>
        <MediaReviewLayout asset={mockAsset} workspaceId="ws-1" onAssetChange={vi.fn()} onClose={vi.fn()} />
      </QueryClientProvider>
    );
    expect(screen.getAllByText(/Version/i)[0]).toBeInTheDocument();
  });
});
