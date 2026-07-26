import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";

// FIR-3782: the disclosure must show what the model actually read. It reaches
// the recorded prompt over the network, so the three states that matter are
// snapshot / no snapshot / request failed — the last two are the COMMON case in
// production (old runs, runtimes that never report, best-effort capture that
// dropped), and both must still show the triggering comment rather than blank.

const mockGetSnapshot = vi.hoisted(() => vi.fn());
const mockFlags = vi.hoisted(() => ({
  cerebro_comment_chapters: true,
  cerebro_run_full_prompt: true,
}));

vi.mock("@multica/cerebro-agent-prompt/api", () => ({
  getAgentPromptSnapshot: mockGetSnapshot,
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: keyof typeof mockFlags) => mockFlags[key],
}));

import { RunPromptDisclosure } from "./run-prompt-disclosure";

const TRIGGER = "Fix the login redirect so it keeps the workspace slug.";

const task = {
  id: "task-1",
  agent_id: "agent-1",
  title: "Run comments",
  trigger_summary: TRIGGER,
  // The component only reads these four fields; the rest of AgentTask is
  // irrelevant to this unit.
} as unknown as Parameters<typeof RunPromptDisclosure>[0]["task"];

function renderDisclosure() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RunPromptDisclosure task={task} />
    </QueryClientProvider>,
  );
}

async function open() {
  await userEvent.click(screen.getByRole("button", { name: /Initial prompt/ }));
}

beforeEach(() => {
  mockGetSnapshot.mockReset();
  mockFlags.cerebro_comment_chapters = true;
  mockFlags.cerebro_run_full_prompt = true;
});

afterEach(cleanup);

describe("RunPromptDisclosure", () => {
  it("shows every recorded layer, one at a time, when a snapshot exists", async () => {
    mockGetSnapshot.mockResolvedValue({
      total_bytes: 3072,
      redacted: true,
      layers: [
        {
          name: "runtime_brief",
          delivery: "workdir_file",
          byte_size: 2048,
          content_redacted: "You are Mia, a coding agent.",
        },
        {
          name: "task_prompt",
          delivery: "user_prompt",
          byte_size: 1024,
          content_redacted: "Fix the login redirect. Reproduce first.",
        },
      ],
    });

    renderDisclosure();
    await open();

    // First layer is shown by default — never all of them at once.
    await waitFor(() =>
      expect(screen.getByText("You are Mia, a coding agent.")).toBeInTheDocument(),
    );
    expect(
      screen.queryByText("Fix the login redirect. Reproduce first."),
    ).not.toBeInTheDocument();

    // Both layers are offered, sized, and the total is stated.
    expect(screen.getByRole("button", { name: /runtime_brief/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText(/3 KB total/)).toBeInTheDocument();
    expect(screen.getByText(/secrets redacted/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /task_prompt/ }));
    expect(
      screen.getByText("Fix the login redirect. Reproduce first."),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("You are Mia, a coding agent."),
    ).not.toBeInTheDocument();
  });

  it("falls back to the triggering comment when no prompt was recorded", async () => {
    mockGetSnapshot.mockResolvedValue(null);

    renderDisclosure();
    await open();

    await waitFor(() => expect(screen.getByText(TRIGGER)).toBeInTheDocument());
    expect(screen.getByText(/No full prompt was recorded/)).toBeInTheDocument();
  });

  it("treats a 404 as 'never recorded', not as a failure", async () => {
    // The server 404s a run with no snapshot. That is the ordinary case for
    // older runs — calling it an error would tell the reader something broke.
    mockGetSnapshot.mockRejectedValue(
      new ApiError("prompt snapshot not found", 404, "Not Found"),
    );

    renderDisclosure();
    await open();

    await waitFor(() => expect(screen.getByText(TRIGGER)).toBeInTheDocument());
    expect(screen.getByText(/No full prompt was recorded/)).toBeInTheDocument();
    expect(screen.queryByText(/could not be loaded/)).not.toBeInTheDocument();
  });

  it("falls back to the triggering comment when the request fails", async () => {
    mockGetSnapshot.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );

    renderDisclosure();
    await open();

    await waitFor(() => expect(screen.getByText(TRIGGER)).toBeInTheDocument());
    expect(screen.getByText(/could not be loaded/)).toBeInTheDocument();
  });

  it("never fetches until the panel is opened", async () => {
    mockGetSnapshot.mockResolvedValue(null);

    renderDisclosure();
    expect(mockGetSnapshot).not.toHaveBeenCalled();

    await open();
    await waitFor(() => expect(mockGetSnapshot).toHaveBeenCalledTimes(1));
  });

  it("renders nothing at all when its own flag is off", () => {
    mockFlags.cerebro_run_full_prompt = false;

    const { container } = renderDisclosure();
    expect(container).toBeEmptyDOMElement();
  });

  it("does not depend on the comment-sessions flag", async () => {
    // The panel used to hang off cerebro_comment_chapters, so showing a run's
    // prompt to a workspace meant also switching on the comment-sessions UI
    // that is off by default and pending a rebuild. The two are now unrelated.
    mockFlags.cerebro_comment_chapters = false;
    mockGetSnapshot.mockResolvedValue(null);

    renderDisclosure();
    await open();

    await waitFor(() => expect(screen.getByText(TRIGGER)).toBeInTheDocument());
  });
});
