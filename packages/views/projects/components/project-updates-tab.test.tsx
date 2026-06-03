import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ProjectUpdatesTab } from "./project-updates-tab";

vi.mock("@multica/core/api", () => ({
  api: {
    listProjectUpdates: vi.fn().mockResolvedValue({ updates: [], total: 0 }),
    createProjectUpdate: vi.fn(),
    deleteProjectUpdate: vi.fn(),
  },
}));

vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "u1" } };
  const useAuthStore = (selector: (s: typeof state) => unknown) => selector(state);
  return { useAuthStore };
});

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("ProjectUpdatesTab", () => {
  beforeEach(() => vi.clearAllMocks());
  it("shows an empty state when there are no updates", async () => {
    wrap(<ProjectUpdatesTab wsId="ws1" projectId="p1" />);
    expect(await screen.findByText(/no updates yet/i)).toBeInTheDocument();
  });
});
