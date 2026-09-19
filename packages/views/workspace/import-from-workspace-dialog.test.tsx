import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { ImportFromWorkspaceDialog } from "./import-from-workspace-dialog";

const mocks = vi.hoisted(() => ({
  workspaces: [
    { id: "ws-current", name: "Current", slug: "current" },
    { id: "ws-other", name: "Studio", slug: "studio" },
  ],
  preview: {
    source_workspace_id: "ws-other",
    source_workspace_name: "Studio",
    agents: [
      {
        id: "a1",
        name: "Lead",
        description: "",
        model: "",
        skill_names: [],
      },
    ],
    squads: [
      {
        id: "s1",
        name: "Delivery",
        description: "",
        leader_id: "a1",
        leader_name: "Lead",
        member_count: 1,
        agent_member_ids: ["a1"],
      },
    ],
  },
  runtimes: [
    {
      id: "rt-1",
      name: "laptop",
      owner_id: "user-1",
      visibility: "public" as const,
    },
  ],
  importFromWorkspace: vi.fn(),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => mocks.workspaces[0],
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQuery: (options: { queryKey?: readonly unknown[]; enabled?: boolean }) => {
    const key = options.queryKey ?? [];
    if (key[0] === "workspaces" && key[1] === "list") {
      return { data: mocks.workspaces, isLoading: false };
    }
    if (key[2] === "import-preview") {
      return { data: mocks.preview, isLoading: false };
    }
    if (key[0] === "runtimes") {
      return { data: mocks.runtimes, isLoading: false };
    }
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: (opts: {
    mutationFn: () => Promise<unknown>;
    onSuccess?: (value: unknown) => void;
  }) => ({
    isPending: false,
    mutate: () => {
      void mocks.importFromWorkspace();
      opts.onSuccess?.({
        agents: [{ source_id: "a1", id: "new-a", name: "Lead", status: "created" }],
        squads: [{ source_id: "s1", id: "new-s", name: "Delivery", status: "created" }],
      });
    },
  }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

describe("ImportFromWorkspaceDialog", () => {
  beforeEach(() => {
    mocks.importFromWorkspace.mockReset();
  });

  it("lists source agents and squads after a workspace is chosen", () => {
    renderWithI18n(<ImportFromWorkspaceDialog open onOpenChange={() => undefined} />);
    fireEvent.change(screen.getByLabelText(/source workspace/i), {
      target: { value: "ws-other" },
    });
    expect(screen.getByText("Lead")).toBeInTheDocument();
    expect(screen.getByText("Delivery")).toBeInTheDocument();
  });

  it("imports the selected agents and squads", () => {
    renderWithI18n(<ImportFromWorkspaceDialog open onOpenChange={() => undefined} />);
    fireEvent.change(screen.getByLabelText(/source workspace/i), {
      target: { value: "ws-other" },
    });
    fireEvent.change(screen.getByLabelText(/runtime for imported agents/i), {
      target: { value: "rt-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^import$/i }));
    expect(mocks.importFromWorkspace).toHaveBeenCalled();
  });
});
