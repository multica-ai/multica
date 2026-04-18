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
});
