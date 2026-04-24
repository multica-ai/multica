import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Hoisted mocks so we can swap getWorkspaceUrlHost's return value per-test.
// The host is driven by a module-level singleton set by CoreProvider. These
// tests don't mount the provider, so we stub the getter directly — mirroring
// the pattern in `onboarding/steps/step-workspace.test.tsx`.
const mocks = vi.hoisted(() => ({
  getWorkspaceUrlHost: vi.fn<() => string>(() => "multica.ai"),
}));

vi.mock("@multica/core/platform", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@multica/core/platform")>();
  return {
    ...actual,
    getWorkspaceUrlHost: mocks.getWorkspaceUrlHost,
  };
});

const mockMutate = vi.fn();
vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({ mutate: mockMutate, isPending: false }),
}));

import { CreateWorkspaceForm } from "./create-workspace-form";

function renderForm(onSuccess = vi.fn()) {
  const qc = new QueryClient();
  return render(
    <QueryClientProvider client={qc}>
      <CreateWorkspaceForm onSuccess={onSuccess} />
    </QueryClientProvider>,
  );
}

describe("CreateWorkspaceForm", () => {
  beforeEach(() => {
    mockMutate.mockReset();
    mocks.getWorkspaceUrlHost.mockReset();
    mocks.getWorkspaceUrlHost.mockReturnValue("multica.ai");
  });

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

  /**
   * The URL pill prefix shown next to the slug input must honor the
   * configurable workspace host (VITE_WORKSPACE_URL_HOST on desktop,
   * NEXT_PUBLIC_WORKSPACE_URL_HOST on web) so rebranded forks can swap
   * `multica.ai` for their own domain. Regression guard: this component
   * previously hardcoded `multica.ai/`.
   */
  describe("configurable URL host", () => {
    it("renders the default 'multica.ai' host in the URL pill when no override is set", () => {
      renderForm();
      expect(screen.getByText("multica.ai/")).toBeInTheDocument();
    });

    it("renders the configured host in the URL pill when overridden", () => {
      mocks.getWorkspaceUrlHost.mockReturnValue("agentfarm.g2.com");
      renderForm();
      expect(screen.getByText("agentfarm.g2.com/")).toBeInTheDocument();
      expect(screen.queryByText("multica.ai/")).not.toBeInTheDocument();
    });

    it("keeps the configured host in the pill while the user types a slug", () => {
      mocks.getWorkspaceUrlHost.mockReturnValue("agentfarm.g2.com");
      renderForm();
      fireEvent.change(screen.getByLabelText(/workspace name/i), {
        target: { value: "Acme Inc" },
      });
      expect(screen.getByText("agentfarm.g2.com/")).toBeInTheDocument();
    });

    it("calls getWorkspaceUrlHost to resolve the host (does not hardcode)", () => {
      renderForm();
      expect(mocks.getWorkspaceUrlHost).toHaveBeenCalled();
    });
  });
});
