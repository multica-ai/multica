/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { Pipeline } from "@multica/core/pipeline";
import { renderWithI18n } from "../test/i18n";
import { InitiativeCreateDialog } from "./initiative-create-dialog";

const api = vi.hoisted(() => ({
  listPipelines: vi.fn(),
  createInitiative: vi.fn(),
}));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const PIPELINES: Pipeline[] = [
  { id: "pl-2", name: "Experiments", is_default: false, stages: [] },
  { id: "pl-1", name: "Default", is_default: true, stages: [] },
];

function renderDialog(onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <InitiativeCreateDialog open onOpenChange={onOpenChange} />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

describe("InitiativeCreateDialog", () => {
  beforeEach(() => {
    api.listPipelines.mockResolvedValue(PIPELINES);
    api.createInitiative.mockReset().mockResolvedValue({ id: "in-9" });
    toasts.success.mockReset();
    toasts.error.mockReset();
  });
  afterEach(cleanup);

  it("preselects the default pipeline", async () => {
    renderDialog();
    expect(await screen.findByRole("dialog", { name: "New initiative" })).toBeTruthy();
    expect(await screen.findByText("Default")).toBeTruthy();
    expect(screen.queryByText("Experiments")).toBeNull();
  });

  it("requires a title, and leaves the thesis optional", async () => {
    renderDialog();
    await screen.findByText("Default");
    fireEvent.click(screen.getByRole("button", { name: "Create initiative" }));

    expect(screen.getByText("A title is required.")).toBeTruthy();
    expect(api.createInitiative).not.toHaveBeenCalled();
  });

  it("creates an initiative without a thesis when none is given", async () => {
    renderDialog();
    await screen.findByText("Default");
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Ledger clean-up" } });
    fireEvent.click(screen.getByRole("button", { name: "Create initiative" }));

    await waitFor(() =>
      expect(api.createInitiative).toHaveBeenCalledWith({ pipeline_id: "pl-1", title: "Ledger clean-up", thesis: "" }),
    );
  });

  it("creates the initiative in the chosen pipeline, then closes", async () => {
    const { onOpenChange } = renderDialog();
    await screen.findByText("Default");

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "  Invoicing for small firms  " } });
    fireEvent.change(screen.getByLabelText("Thesis"), { target: { value: "Month-end close is slow." } });
    fireEvent.click(screen.getByRole("button", { name: "Create initiative" }));

    await waitFor(() =>
      expect(api.createInitiative).toHaveBeenCalledWith({
        pipeline_id: "pl-1",
        title: "Invoicing for small firms",
        thesis: "Month-end close is slow.",
      }),
    );
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(toasts.success).toHaveBeenCalledWith("Initiative created.");
  });

  it("keeps the input open with the sentence for a missing pipeline", async () => {
    api.createInitiative.mockRejectedValue(
      new ApiError("no default pipeline", 409, "Conflict", { error: "no default pipeline", code: "no_pipeline" }),
    );
    const { onOpenChange } = renderDialog();
    await screen.findByText("Default");

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Invoicing" } });
    fireEvent.change(screen.getByLabelText("Thesis"), { target: { value: "Why" } });
    fireEvent.click(screen.getByRole("button", { name: "Create initiative" }));

    expect(await screen.findByText("This workspace has no pipeline to start an initiative in.")).toBeTruthy();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Invoicing");
  });
});
