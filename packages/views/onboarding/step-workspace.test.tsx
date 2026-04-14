import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockCreateWorkspaceMutate = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({
    mutate: mockCreateWorkspaceMutate,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    error: mockToastError,
  },
}));

import { StepWorkspace } from "./step-workspace";

describe("StepWorkspace", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("asks the user to change the slug on conflict", async () => {
    const user = userEvent.setup();
    mockCreateWorkspaceMutate.mockImplementation(
      (
        _data: unknown,
        options: { onError: (error: unknown) => void },
      ) => {
        options.onError({ status: 409 });
      },
    );

    render(<StepWorkspace onNext={vi.fn()} />);

    await user.type(screen.getByPlaceholderText("My Team"), "My Team");
    await user.click(screen.getByRole("button", { name: "Create Workspace" }));

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
});
