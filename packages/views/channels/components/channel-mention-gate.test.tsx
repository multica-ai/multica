// FIR-2680 — behaviour of the channel mention-membership gate. The hook lives
// in @multica/cerebro-channels; this jsdom test lives in views (repo rule:
// jsdom UI tests live here, mocking @multica/core).
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ChannelMember } from "@multica/core/types";
import { useChannelMentionGate } from "@multica/cerebro-channels";

const mockToggle = vi.hoisted(() => vi.fn());
const flagState = vi.hoisted(() => ({ on: true }));

vi.mock("@multica/core/channels", () => ({
  useToggleChannelParticipant: () => ({ mutate: mockToggle }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_t: string, id: string) => `User-${id}` }),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => flagState.on,
}));

const MEMBER = "11111111-1111-1111-1111-111111111111";
const OUTSIDER = "22222222-2222-2222-2222-222222222222";

// Test harness: renders the gate's dialog and exposes confirmBeforeSend so the
// test can drive a send and inspect the resolved decision.
function Harness({
  participants,
  onResolve,
  markdown,
}: {
  participants: ChannelMember[];
  onResolve: (proceed: boolean) => void;
  markdown: string;
}) {
  const gate = useChannelMentionGate("chan-1", participants);
  return (
    <div>
      <button
        onClick={() => {
          void gate.confirmBeforeSend(markdown).then(onResolve);
        }}
      >
        send
      </button>
      {gate.confirmDialog}
    </div>
  );
}

describe("useChannelMentionGate", () => {
  beforeEach(() => {
    mockToggle.mockReset();
    flagState.on = true;
  });

  const draftTagging = (id: string) => `hi [@user](mention://member/${id})`;

  it("passes through with no dialog when every mention is already a participant", async () => {
    const onResolve = vi.fn();
    const participants: ChannelMember[] = [{ user_type: "member", user_id: MEMBER }];
    render(<Harness participants={participants} onResolve={onResolve} markdown={draftTagging(MEMBER)} />);

    await userEvent.click(screen.getByText("send"));

    expect(screen.queryByText("Add to this channel?")).not.toBeInTheDocument();
    expect(onResolve).toHaveBeenCalledWith(true);
    expect(mockToggle).not.toHaveBeenCalled();
  });

  it("passes through with no dialog when the flag is off, even for a non-participant", async () => {
    flagState.on = false;
    const onResolve = vi.fn();
    render(<Harness participants={[]} onResolve={onResolve} markdown={draftTagging(OUTSIDER)} />);

    await userEvent.click(screen.getByText("send"));

    expect(screen.queryByText("Add to this channel?")).not.toBeInTheDocument();
    expect(onResolve).toHaveBeenCalledWith(true);
  });

  it("shows the dialog for a non-participant mention", async () => {
    render(<Harness participants={[]} onResolve={vi.fn()} markdown={draftTagging(OUTSIDER)} />);

    await userEvent.click(screen.getByText("send"));

    expect(await screen.findByText("Add to this channel?")).toBeInTheDocument();
  });

  it("'Send without' resolves true and does not subscribe anyone", async () => {
    const onResolve = vi.fn();
    render(<Harness participants={[]} onResolve={onResolve} markdown={draftTagging(OUTSIDER)} />);

    await userEvent.click(screen.getByText("send"));
    await userEvent.click(await screen.findByText("Send without"));

    expect(onResolve).toHaveBeenCalledWith(true);
    expect(mockToggle).not.toHaveBeenCalled();
  });

  it("'Add & send' subscribes the missing member then resolves true", async () => {
    // The add mutation reports success so the gate resolves.
    mockToggle.mockImplementation((_vars, opts?: { onSuccess?: () => void }) => opts?.onSuccess?.());
    const onResolve = vi.fn();
    render(<Harness participants={[]} onResolve={onResolve} markdown={draftTagging(OUTSIDER)} />);

    await userEvent.click(screen.getByText("send"));
    await userEvent.click(await screen.findByText("Add & send"));

    expect(mockToggle).toHaveBeenCalledWith(
      expect.objectContaining({
        channelId: "chan-1",
        action: "add",
        member: { user_type: "member", user_id: OUTSIDER },
      }),
      expect.anything(),
    );
    expect(onResolve).toHaveBeenCalledWith(true);
  });

  it("'Cancel' resolves false and keeps the draft (no send)", async () => {
    const onResolve = vi.fn();
    render(<Harness participants={[]} onResolve={onResolve} markdown={draftTagging(OUTSIDER)} />);

    await userEvent.click(screen.getByText("send"));
    await userEvent.click(await screen.findByText("Cancel"));

    expect(onResolve).toHaveBeenCalledWith(false);
    expect(mockToggle).not.toHaveBeenCalled();
  });
});
