import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { ProjectChip } from "./project-chip";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
  projectDetailOptions: (_workspaceId: string, projectId: string) => ({
    queryKey: ["project", projectId],
  }),
}));

vi.mock("./project-icon", () => ({
  ProjectIcon: ({ className }: { className?: string }) => (
    <span data-testid="project-icon" className={className} />
  ),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "Unknown project" }),
}));

const mockUseQuery = vi.mocked(useQuery);

const LONG_TITLE =
  "A very long project title that should stay inside a narrow chat bubble";

describe("ProjectChip", () => {
  beforeEach(() => {
    mockUseQuery.mockImplementation((options: { queryKey?: readonly unknown[] }) => {
      if (options.queryKey?.[0] === "projects") {
        return {
          data: [{ id: "project-1", title: LONG_TITLE, emoji: "🚀" }],
        } as ReturnType<typeof useQuery>;
      }
      return { data: undefined } as ReturnType<typeof useQuery>;
    });
  });

  // ProjectChip and IssueChip render the same inline-chip shape in the same
  // prose. If only one of them bounds its width, reference-dense content breaks
  // for that one — which is how #6732 happened. Locking the cap in both tests
  // is what keeps them from drifting apart again.
  it("carries the same width cap as IssueChip and truncates the title", () => {
    render(<ProjectChip projectId="project-1" />);

    const chip = screen.getByText(LONG_TITLE).closest(".project-chip");
    expect(chip).toHaveClass("min-w-0");
    expect(chip).toHaveClass("max-w-[min(18rem,100%)]");
    expect(screen.getByText(LONG_TITLE)).toHaveClass("min-w-0", "truncate");
  });

  it("truncates the fallback label inside the chip width", () => {
    render(<ProjectChip projectId="missing-project" fallbackLabel={LONG_TITLE} />);

    expect(screen.getByText(LONG_TITLE)).toHaveClass("min-w-0", "truncate");
  });
});
