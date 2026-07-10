import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

// SessionHeader calls useUpdateSession(issueId) at render; stub it so the
// component renders without a QueryClient/API round-trip.
vi.mock("./use-sessions", () => ({
  useUpdateSession: () => ({ mutateAsync: vi.fn() }),
}));

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
  phase,
  handoff: null,
  created_at: "2026-06-21T00:00:00Z",
  updated_at: "2026-06-21T00:00:00Z",
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
