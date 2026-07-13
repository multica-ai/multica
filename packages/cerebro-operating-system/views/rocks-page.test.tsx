import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock } from "../core/types";
import { RocksPage } from "./rocks-page";

const state = vi.hoisted(() => ({
  enabled: true,
  rocks: [] as Rock[],
  terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" },
  rocksLoading: false,
  rocksError: false,
  projects: [{ id: "project-1", title: "North star project", status: "in_progress" }],
  mutate: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => state.enabled,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects", "workspace-1", "list"] }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly string[] }) => {
      if (options.queryKey.includes("settings")) return { data: { terminology: state.terminology } };
      if (options.queryKey.includes("rocks")) {
        return {
          data: { rocks: state.rocks },
          isLoading: state.rocksLoading,
          isError: state.rocksError,
        };
      }
      return { data: state.projects, isLoading: false };
    },
  };
});

vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return {
    ...actual,
    useUpsertRock: () => ({ mutate: state.mutate, isPending: false }),
  };
});

const rock: Rock = {
  project_id: "project-1",
  workspace_id: "workspace-1",
  project_title: "A deliberately long project title that must wrap instead of widening the page",
  project_description: "Make the operating rhythm measurable.",
  project_status: "active",
  period_start: "2026-07-01",
  period_end: "2026-09-30",
  confidence: 72,
  reported_health: "at_risk",
  derived_health: {
    state: "off_track",
    reason: "2 blocked issues require attention",
    calculated_at: "2026-07-13T18:00:00Z",
  },
  issue_count: 5,
  done_issue_count: 2,
  blocked_issue_count: 2,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-13T18:00:00Z",
};

describe("RocksPage", () => {
  beforeEach(() => {
    state.enabled = true;
    state.rocks = [];
    state.terminology = { strategy: "Strategy", rock: "Rock", rocks: "Rocks" };
    state.rocksLoading = false;
    state.rocksError = false;
    state.mutate.mockReset();
  });

  it("stays hidden when the feature flag is off", () => {
    state.enabled = false;
    const { container } = render(<RocksPage />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders loading, error, and terminology-aware empty states", () => {
    state.rocksLoading = true;
    const { rerender } = render(<RocksPage />);
    expect(screen.getByText("Loading Rocks…")).toBeInTheDocument();

    state.rocksLoading = false;
    state.rocksError = true;
    rerender(<RocksPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("Rocks could not be loaded");

    state.rocksError = false;
    state.terminology = { strategy: "Direction", rock: "Priority", rocks: "Priorities" };
    rerender(<RocksPage />);
    expect(screen.getByText("No Priorities yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add Priority" })).toBeInTheDocument();
  });

  it("shows project-owned progress and explains derived health", () => {
    state.rocks = [rock];
    render(<RocksPage />);

    expect(screen.getByText(rock.project_title)).toHaveClass("break-words");
    expect(screen.getByText("2 of 5 issues done")).toBeInTheDocument();
    expect(screen.getByText("2 blocked")).toBeInTheDocument();
    expect(screen.getByText("72% confidence")).toBeInTheDocument();
    expect(screen.getByText("Sep 30, 2026")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Why off track?" }));
    expect(screen.getByText("2 blocked issues require attention")).toBeInTheDocument();
    expect(screen.getByText(/Calculated Jul 13, 2026/)).toBeInTheDocument();
  });

  it("marks an existing Project as a Rock without asking for a duplicate title", () => {
    render(<RocksPage />);
    fireEvent.click(screen.getByRole("button", { name: "Add Rock" }));

    expect(screen.queryByLabelText("Rock title")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "project-1" } });
    fireEvent.change(screen.getByLabelText("Period start"), { target: { value: "2026-07-01" } });
    fireEvent.change(screen.getByLabelText("Period end"), { target: { value: "2026-09-30" } });
    fireEvent.change(screen.getByLabelText("Confidence"), { target: { value: "80" } });
    fireEvent.change(screen.getByLabelText("Reported health"), { target: { value: "on_track" } });
    fireEvent.click(screen.getByRole("button", { name: "Save Rock" }));

    expect(state.mutate).toHaveBeenCalledWith(
      {
        project_id: "project-1",
        period_start: "2026-07-01",
        period_end: "2026-09-30",
        confidence: 80,
        reported_health: "on_track",
      },
      expect.any(Object),
    );
  });
});
