// CEREBRO-PATCH(channels-edit-channel-dialog-test): FIR-2660 unit test for EditChannelDialog.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Channel } from "@multica/core/types";

const mockUpdate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/channels", () => ({
  useUpdateChannel: () => ({ mutate: mockUpdate, isPending: false }),
}));

import { EditChannelDialog } from "./edit-channel-dialog";

const channel: Channel = {
  id: "c1",
  workspace_id: "ws",
  number: 42,
  identifier: "TECH-42",
  kind: "channel",
  title: "general",
  description: "Old topic",
  status: "todo",
  project_id: null,
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "me",
  participants: [{ user_type: "member", user_id: "me" }],
  unread_count: 0,
  last_message: null,
  created_at: "2026-06-03T00:00:00Z",
  updated_at: "2026-06-03T00:00:00Z",
};

describe("EditChannelDialog", () => {
  beforeEach(() => {
    mockUpdate.mockReset();
  });

  it("disables Save until something changes", async () => {
    const user = userEvent.setup();
    render(<EditChannelDialog channel={channel} open onOpenChange={() => {}} />);

    const save = screen.getByRole("button", { name: /save/i });
    expect(save).toBeDisabled();

    await user.type(screen.getByLabelText(/channel name/i), "-updates");
    expect(save).toBeEnabled();
  });

  it("sends only the changed fields to useUpdateChannel", async () => {
    const user = userEvent.setup();
    render(<EditChannelDialog channel={channel} open onOpenChange={() => {}} />);

    await user.clear(screen.getByLabelText(/channel description/i));
    await user.type(screen.getByLabelText(/channel description/i), "New topic");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(mockUpdate).toHaveBeenCalledTimes(1);
    const call = mockUpdate.mock.calls[0]!;
    expect(call[0]).toEqual({ id: "c1", description: "New topic" });
  });

  it("blocks Save when the name is empty", async () => {
    const user = userEvent.setup();
    render(<EditChannelDialog channel={channel} open onOpenChange={() => {}} />);

    await user.clear(screen.getByLabelText(/channel name/i));
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled();
  });

  it("closes the dialog on successful save", async () => {
    const onOpenChange = vi.fn();
    mockUpdate.mockImplementation((_payload, opts) => {
      opts?.onSuccess?.();
    });

    const user = userEvent.setup();
    render(<EditChannelDialog channel={channel} open onOpenChange={onOpenChange} />);

    await user.type(screen.getByLabelText(/channel name/i), "-v2");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
