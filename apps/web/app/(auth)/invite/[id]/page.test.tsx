import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import InviteAcceptPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({ id: "inv-1" }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: object; isLoading: boolean }) => unknown) =>
    selector({ user: { id: "u-1" }, isLoading: false }),
}));

vi.mock("@multica/core/api", () => ({
  api: { listWorkspaces: vi.fn().mockResolvedValue([]) },
}));

vi.mock("@multica/views/invitations", () => ({
  InvitationsPage: () => <div data-testid="invitations-page" />,
}));

describe("InviteAcceptPage", () => {
  // The root layout sets `body { overflow: hidden }`, so a long invitation
  // list is only reachable if the route owns its scroll container.
  it("renders the invitations inside a page-owned scroll container", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <InviteAcceptPage />
      </QueryClientProvider>,
    );

    const scroller = screen.getByTestId("invitations-page").parentElement!;
    expect(scroller).toHaveClass("h-full", "overflow-y-auto");
  });
});
