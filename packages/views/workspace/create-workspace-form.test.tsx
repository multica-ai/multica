import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CreateWorkspaceForm } from "./create-workspace-form";

const mockMutate = vi.fn();
vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({ mutate: mockMutate, isPending: false }),
}));

function renderForm(onSuccess = vi.fn()) {
  const qc = new QueryClient();
  return render(
    <QueryClientProvider client={qc}>
      <CreateWorkspaceForm onSuccess={onSuccess} />
    </QueryClientProvider>,
  );
}

describe("CreateWorkspaceForm", () => {
  beforeEach(() => mockMutate.mockReset());

  it("auto-generates slug from name until user edits slug", () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme Corp" },
    });
    expect(screen.getByDisplayValue("acme-corp")).toBeInTheDocument();
  });

  it("stops auto-generating slug once user edits slug directly", () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace url/i), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Different Name" },
    });
    expect(screen.getByDisplayValue("custom")).toBeInTheDocument();
  });

  it("calls onSuccess with the created workspace", async () => {
    const onSuccess = vi.fn();
    mockMutate.mockImplementation((_args, opts) => {
      opts?.onSuccess?.({ id: "ws-1", slug: "acme", name: "Acme" });
    });
    renderForm(onSuccess);
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme" },
    });
    fireEvent.click(screen.getByRole("button", { name: /create workspace/i }));
    await waitFor(() =>
      expect(onSuccess).toHaveBeenCalledWith(
        expect.objectContaining({ slug: "acme" }),
      ),
    );
  });

  it("shows slug-conflict error inline on 409", async () => {
    mockMutate.mockImplementation((_args, opts) => {
      opts?.onError?.({ status: 409 });
    });
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Taken" },
    });
    fireEvent.click(screen.getByRole("button", { name: /create workspace/i }));
    await waitFor(() =>
      expect(screen.getByText(/already taken/i)).toBeInTheDocument(),
    );
  });

  it("disables submit when slug has invalid format", () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Valid Name" },
    });
    fireEvent.change(screen.getByLabelText(/workspace url/i), {
      target: { value: "Invalid Slug!" },
    });
    expect(
      screen.getByRole("button", { name: /create workspace/i }),
    ).toBeDisabled();
  });

  it("does not send local_path when create-from-folder is disabled", async () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme Root" },
    });

    fireEvent.click(screen.getByRole("button", { name: /create workspace/i }));

    await waitFor(() => expect(mockMutate).toHaveBeenCalledTimes(1));
    expect(mockMutate.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        name: "Acme Root",
        slug: "acme-root",
      }),
    );
    expect(mockMutate.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        local_path: undefined,
      }),
    );
  });

  it("requires folder path when create-from-folder is enabled", () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme Root" },
    });

    fireEvent.click(screen.getByRole("checkbox"));
    expect(
      screen.getByRole("button", { name: /create workspace/i }),
    ).toBeDisabled();
  });

  it("trims and sends local_path when create-from-folder is enabled", async () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme Root" },
    });
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.change(screen.getByLabelText(/folder path/i), {
      target: { value: "  /home/user/projects/acme-root  " },
    });

    fireEvent.click(screen.getByRole("button", { name: /create workspace/i }));

    await waitFor(() => expect(mockMutate).toHaveBeenCalledTimes(1));
    expect(mockMutate.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        name: "Acme Root",
        slug: "acme-root",
        local_path: "/home/user/projects/acme-root",
      }),
    );
  });
});
