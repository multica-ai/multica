import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateWorkspaceMutate = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({
    mutate: mockCreateWorkspaceMutate,
    isPending: false,
  }),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  ),
}));

vi.mock("sonner", () => ({
  toast: {
    error: mockToastError,
  },
}));

vi.mock("@multica/i18n/react", async () => {
  const { en } = await import("@multica/i18n/dict/en");
  return {
    useT: (ns?: string) => (key: string, params?: Record<string, string | number>) => {
      const template = ns ? en[ns]?.[key] ?? key : key;
      if (!params) return template;
      return template.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? `{${k}}`));
    },
  };
});

import { CreateWorkspaceModal } from "./create-workspace";

describe("CreateWorkspaceModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("auto-generates the slug until the user edits it", async () => {
    const user = userEvent.setup();
    render(<CreateWorkspaceModal onClose={vi.fn()} />);

    const nameInput = screen.getByPlaceholderText("My Workspace");
    const slugInput = screen.getByPlaceholderText("my-workspace");

    await user.type(nameInput, "My Team");
    expect(slugInput).toHaveValue("my-team");

    await user.clear(slugInput);
    await user.type(slugInput, "custom-team");
    await user.clear(nameInput);
    await user.type(nameInput, "Renamed Team");

    expect(slugInput).toHaveValue("custom-team");
  });

  it("shows a specific slug conflict error on 409 responses", async () => {
    const user = userEvent.setup();
    mockCreateWorkspaceMutate.mockImplementation(
      (
        _data: unknown,
        options: { onError: (error: unknown) => void },
      ) => {
        options.onError({ status: 409 });
      },
    );

    render(<CreateWorkspaceModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("My Workspace"), "My Team");
    await user.click(screen.getByRole("button", { name: "Create workspace" }));

    await waitFor(() => {
      expect(
        screen.getByText("That workspace URL is already taken."),
      ).toBeInTheDocument();
    });

    expect(mockToastError).toHaveBeenCalledWith(
      "Choose a different workspace URL",
    );
    expect(mockCreateWorkspaceMutate).toHaveBeenCalledWith(
      { name: "My Team", slug: "my-team" },
      expect.any(Object),
    );
  });

  it("navigates into the newly created workspace after success", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockCreateWorkspaceMutate.mockImplementation(
      (
        _data: unknown,
        options: { onSuccess: (ws: { slug: string }) => void },
      ) => {
        options.onSuccess({ slug: "my-team" });
      },
    );

    render(<CreateWorkspaceModal onClose={onClose} />);

    await user.type(screen.getByPlaceholderText("My Workspace"), "My Team");
    await user.click(screen.getByRole("button", { name: "Create workspace" }));

    expect(onClose).toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith("/my-team/issues");
  });
});
