import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// SessionHeader calls useUpdateSession(issueId) at render; stub it so the
// component renders without a QueryClient/API round-trip.
const mutateAsync = vi.fn();
vi.mock("./use-sessions", () => ({ useUpdateSession: () => ({ mutateAsync }) }));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));

import { SessionHeader } from "./session-header";
import type { Session } from "./types";

// Globals are off in this package, so testing-library's afterEach auto-cleanup
// does not fire — unmount between tests so a prior render's badge does not leak.
afterEach(cleanup);

const session = (phase: string | null): Session => ({
  id: "root-1",
  issue_id: "issue-1",
  root_comment_id: "root-1",
  position: 1,
  name: "A session",
  mode: "build",
  phase,
  handoff: null,
  created_at: "2026-06-21T00:00:00Z",
  updated_at: "2026-06-21T00:00:00Z",
});

describe("SessionHeader mode select", () => {
  it("switches an ordinary session into Research mode via the mode select", async () => {
    render(<SessionHeader issueId="issue-1" session={session(null)} open onToggle={() => {}} />);
    const trigger = screen.getByRole("combobox", { name: "Session mode" });
    expect(trigger).toHaveTextContent("Build");
    await userEvent.click(trigger);
    expect((await screen.findAllByRole("option")).map((option) => option.textContent)).toEqual([
      "Plan",
      "Build",
      "Research",
      "Review",
    ]);
    await userEvent.click(await screen.findByRole("option", { name: "Research" }));
    expect(mutateAsync).toHaveBeenCalledWith({ sessionId: "root-1", input: { mode: "research" } });
  });

  it("renders a legacy default session as Build without offering a misleading Auto mode", async () => {
    render(
      <SessionHeader
        issueId="issue-1"
        session={{ ...session(null), mode: "default" as never }}
        open
        onToggle={() => {}}
      />,
    );
    const trigger = screen.getByRole("combobox", { name: "Session mode" });
    expect(trigger).toHaveTextContent("Build");
    await userEvent.click(trigger);
    expect(screen.queryByRole("option", { name: "Auto" })).not.toBeInTheDocument();
  });
});

describe("SessionHeader thread state", () => {
  it("presents Handoff as the reason a thread is Resolved, not as a separate status", async () => {
    const onToggleHandoff = vi.fn();
    render(
      <SessionHeader
        issueId="issue-1"
        session={{
          ...session(null),
          handoff: { summary: "Continue in the fresh thread", done: [], remaining: [] },
        }}
        open
        resolved
        hasHandoff
        handoffOpen={false}
        onToggle={() => {}}
        onToggleHandoff={onToggleHandoff}
      />,
    );

    expect(screen.queryByRole("button", { name: "Handoff" })).not.toBeInTheDocument();
    const state = screen.getByRole("button", { name: "Resolved via Handoff" });
    await userEvent.click(state);
    expect(onToggleHandoff).toHaveBeenCalledOnce();
  });
});

describe("SessionHeader phase badge", () => {
  it("renders the phase badge when the session has a phase", () => {
    render(
      <SessionHeader
        issueId="issue-1"
        session={session("plan")}
        open
        onToggle={() => {}}
      />,
    );
    const badge = screen.getByTestId("session-phase-badge");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveTextContent("Plan");
  });

  it("renders no phase badge when the session has no phase", () => {
    render(
      <SessionHeader
        issueId="issue-1"
        session={session(null)}
        open
        onToggle={() => {}}
      />,
    );
    expect(screen.queryByTestId("session-phase-badge")).not.toBeInTheDocument();
  });
});
