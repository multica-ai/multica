import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ role: "member" as string, create: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", () => ({ useCurrentMember: () => ({ role: state.role }) }));
vi.mock("@multica/core/quick-actions", () => ({ useCreateQuickAction: () => ({ mutateAsync: state.create }) }));
vi.mock("../../autopilots/components/pickers/agent-picker", () => ({ AgentPicker: () => null }));
import type { QuickAction } from "@multica/core/types";
import { CreateQuickActionDialog, QuickActionDialog } from "./quick-action-dialog";

describe("shared quick action creation", () => {
  beforeEach(() => { state.role = "member"; state.create.mockReset(); state.create.mockResolvedValue({}); });
  function fill() {
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Review" } });
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "Review this issue" } });
  }
  it("lets a member create a private action and closes after success", async () => {
    const close = vi.fn();
    renderWithI18n(<CreateQuickActionDialog onClose={close} />);
    expect(screen.queryByRole("button", { name: /Team/ })).not.toBeInTheDocument();
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    expect(state.create).toHaveBeenCalledWith(expect.objectContaining({ visibility: "private", name: "Review", prompt: "Review this issue" }));
  });
  it("allows an admin to choose a team action", async () => {
    state.role = "admin";
    renderWithI18n(<CreateQuickActionDialog onClose={vi.fn()} />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: /Team/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(state.create).toHaveBeenCalledWith(expect.objectContaining({ visibility: "public" })));
  });
  it("preserves the draft after a failed creation", async () => {
    state.create.mockRejectedValue(new Error("Save failed"));
    const close = vi.fn();
    renderWithI18n(<CreateQuickActionDialog onClose={close} />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeEnabled());
    expect(close).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Prompt")).toHaveValue("Review this issue");
  });
  it("blocks empty and template prompts before submission", () => {
    renderWithI18n(<CreateQuickActionDialog onClose={vi.fn()} />);
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    fill();
    expect(save).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "Review {{issue.title}}" } });
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(state.create).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "   " } });
    expect(save).toBeDisabled();
  });

  it("preserves saved visibility when editing and resets a reopened new draft", async () => {
    const existing: QuickAction = {
      id: "qa-1", workspace_id: "ws-1", name: "Existing action", description: "",
      assignee_type: "", assignee_id: "", prompt: "Existing prompt", visibility: "public",
      status: "active", created_by_id: "user-1", created_at: "", updated_at: "",
      last_used_at: null, use_count: 0, target_name: "", target_public: false, target_missing: false,
    };
    const submit = vi.fn().mockResolvedValue(undefined);
    const change = vi.fn();
    const view = renderWithI18n(<QuickActionDialog open action={existing} onOpenChange={change} onSubmit={submit} />);
    expect(screen.getByRole("button", { name: /Team/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Name")).toHaveValue("Existing action");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(submit).toHaveBeenCalledWith(expect.objectContaining({ visibility: "public" })));
    await waitFor(() => expect(change).toHaveBeenCalledWith(false));
    view.rerender(<QuickActionDialog open={false} action={null} onOpenChange={change} onSubmit={submit} />);
    view.rerender(<QuickActionDialog open action={null} onOpenChange={change} onSubmit={submit} />);
    expect(screen.getByLabelText("Name")).toHaveValue("");
    expect(screen.getByRole("button", { name: /Just me/ })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(submit).toHaveBeenCalledTimes(1);
  });

});
