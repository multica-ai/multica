import { describe, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MergeRequestList } from "./merge-request-list";

vi.mock("@multica/core/gitlab", () => ({
  issueMergeRequestsOptions: (issueId: string) => ({
    queryKey: ["gitlab", "merge-requests", issueId],
    queryFn: async () => ({ merge_requests: [] }),
    enabled: !!issueId,
  }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("MergeRequestList", () => {
  it("renders without crashing when empty", () => {
    render(<MergeRequestList issueId="abc" />, { wrapper });
    // No MR rows — nothing to assert beyond no error
  });
});
