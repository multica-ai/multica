import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import IssuesLayout from "./layout";

const route = { id: "issue-1" as string | null };
const { unmounted } = vi.hoisted(() => ({ unmounted: vi.fn() }));

vi.mock("next/navigation", () => ({
  useSelectedLayoutSegment: () => route.id,
}));

vi.mock("@multica/views/issues/components", async () => {
  const { useEffect, useState } = await import("react");
  return {
    IssueDetailRoute: ({ routeId }: { routeId: string }) => {
      const [instance] = useState(() => crypto.randomUUID());
      useEffect(() => unmounted, []);
      return <div data-testid="detail">{`${routeId}:${instance}`}</div>;
    },
  };
});

vi.mock("@multica/ui/components/common/error-boundary", () => ({
  ErrorBoundary: ({ children }: { children: React.ReactNode }) => children,
}));

describe("IssuesLayout", () => {
  it("keeps the last detail surface mounted across list re-entry", () => {
    const view = render(<IssuesLayout>Issue list</IssuesLayout>);
    const instance = view.getByTestId("detail").textContent!.split(":")[1];

    route.id = null;
    view.rerender(<IssuesLayout><div>Issue list</div></IssuesLayout>);
    expect(view.getByText("Issue list")).toBeInTheDocument();
    expect(unmounted).not.toHaveBeenCalled();

    route.id = "MUL-1";
    view.rerender(<IssuesLayout>Issue list</IssuesLayout>);
    expect(view.getByTestId("detail")).toHaveTextContent(`MUL-1:${instance}`);
    expect(unmounted).not.toHaveBeenCalled();
  });
});
