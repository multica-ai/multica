// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Skill } from "@multica/core/types";

const mockImportSkill = vi.hoisted(() => vi.fn());
const mockToastCustom = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      importSkill: (...args: unknown[]) => mockImportSkill(...args),
    },
  };
});

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({
    render,
    children,
  }: {
    render?: ReactNode;
    children?: ReactNode;
  }) => <>{render ?? children}</>,
}));

vi.mock("sonner", () => ({
  toast: {
    custom: mockToastCustom,
    dismiss: mockToastDismiss,
    success: mockToastSuccess,
    error: vi.fn(),
  },
}));

vi.mock("../../platform", () => ({
  openExternal: vi.fn(),
}));

import { ApiError } from "@multica/core/api";
import { CreateSkillDialog } from "./create-skill-dialog";

const importedSkill: Skill = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "shadcn",
  description: "",
  content: "",
  config: {},
  files: [],
  created_by: "user-1",
  created_at: "2026-04-26T00:00:00Z",
  updated_at: "2026-04-26T00:00:00Z",
};

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <CreateSkillDialog onClose={vi.fn()} />
    </QueryClientProvider>,
  );
}

describe("CreateSkillDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("asks for confirmation before skipping unsupported import files", async () => {
    const user = userEvent.setup();
    mockImportSkill
      .mockRejectedValueOnce(
        new ApiError(
          "skill import contains files that cannot be stored as text",
          409,
          "Conflict",
          {
            error: "skill import contains files that cannot be stored as text",
            code: "skill_import_skipped_files",
            skipped_files: [
              "assets/shadcn.png",
              "assets/shadcn-small.png",
            ],
          },
        ),
      )
      .mockResolvedValueOnce(importedSkill);

    renderDialog();

    await user.click(screen.getByRole("button", { name: /Import from URL/i }));
    await user.type(
      screen.getByLabelText("Skill URL"),
      "https://skills.sh/shadcn/ui/shadcn",
    );
    await user.click(screen.getByRole("button", { name: /^Import$/i }));

    await waitFor(() => {
      expect(mockToastCustom).toHaveBeenCalledTimes(1);
    });
    expect(mockImportSkill).toHaveBeenCalledWith({
      url: "https://skills.sh/shadcn/ui/shadcn",
      allow_skipped_files: undefined,
    });

    const [renderToast] = mockToastCustom.mock.calls[0]!;
    render(<>{renderToast("skip-toast")}</>);

    expect(
      screen.getByText("Some files cannot be imported"),
    ).toBeInTheDocument();
    expect(screen.getByText("assets/shadcn.png")).toBeInTheDocument();
    expect(screen.getByText("assets/shadcn-small.png")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      expect(mockImportSkill).toHaveBeenLastCalledWith({
        url: "https://skills.sh/shadcn/ui/shadcn",
        allow_skipped_files: true,
      });
    });
    expect(mockToastDismiss).toHaveBeenCalledWith("skip-toast");
    expect(mockToastSuccess).toHaveBeenCalledWith("Skill imported");
  });
});
