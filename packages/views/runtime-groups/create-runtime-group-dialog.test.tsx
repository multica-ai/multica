import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { CreateRuntimeGroupDialog } from "./create-runtime-group-dialog";
import type { RuntimeDevice, MemberWithUser } from "@multica/core/types";

const runtime = (id: string, name: string): RuntimeDevice => ({
  id,
  workspace_id: "ws",
  daemon_id: null,
  name,
  runtime_mode: "local",
  provider: "claude",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: null,
  last_seen_at: null,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
});

describe("CreateRuntimeGroupDialog", () => {
  it("disables Create until name + at least one runtime are set", async () => {
    render(
      <CreateRuntimeGroupDialog
        runtimes={[runtime("r1", "Workstation")]}
        members={[] as MemberWithUser[]}
        currentUserId="u1"
        onClose={vi.fn()}
        onCreate={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: /^create$/i })).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText(/Backend Team/i), { target: { value: "Team A" } });
    fireEvent.click(screen.getByRole("button", { name: /add runtime/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /Workstation/i }));
    expect(screen.getByRole("button", { name: /^create$/i })).not.toBeDisabled();
  });

  it("submits the selected runtime_ids", async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(
      <CreateRuntimeGroupDialog
        runtimes={[runtime("r1", "R1"), runtime("r2", "R2")]}
        members={[] as MemberWithUser[]}
        currentUserId="u1"
        onClose={vi.fn()}
        onCreate={onCreate}
      />,
    );
    fireEvent.change(screen.getByPlaceholderText(/Backend Team/i), { target: { value: "T" } });
    fireEvent.click(screen.getByRole("button", { name: /add runtime/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /R1/i }));
    fireEvent.click(screen.getByRole("button", { name: /add runtime/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /R2/i }));
    fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
    await waitFor(() => expect(onCreate).toHaveBeenCalled());
    expect(onCreate).toHaveBeenCalledWith({
      name: "T",
      description: "",
      runtime_ids: ["r1", "r2"],
    });
  });
});
