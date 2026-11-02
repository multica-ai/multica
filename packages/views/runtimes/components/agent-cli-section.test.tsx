// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { runtimeCLIStatusOptions } from "@multica/core/runtimes/queries";
import type { AgentCLIStatus, RuntimeCLIStatusResponse } from "@multica/core/types";
import { AgentCliSection } from "./agent-cli-section";
import { renderWithI18n } from "../../test/i18n";

const mockInitiate = vi.hoisted(() => vi.fn());
const mockGetJob = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    getRuntimeCLIStatus: vi.fn(),
    initiateAgentCLIUpgrade: (...args: unknown[]) => mockInitiate(...args),
    getAgentCLIUpgrade: (...args: unknown[]) => mockGetJob(...args),
  },
}));

function row(overrides: Partial<AgentCLIStatus>): AgentCLIStatus {
  return {
    cli: "codex",
    current: "1.0.0",
    latest: "1.0.0",
    status: "up_to_date",
    upgradable: false,
    channel: "npm",
    ...overrides,
  };
}

function renderSection(
  clis: AgentCLIStatus[],
  opts: { isOnline?: boolean; canUpgrade?: boolean } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const payload: RuntimeCLIStatusResponse = {
    runtime_id: "runtime-1",
    clis,
  };
  queryClient.setQueryData(
    runtimeCLIStatusOptions("runtime-1").queryKey,
    payload,
  );

  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <AgentCliSection
        runtimeId="runtime-1"
        isOnline={opts.isOnline ?? true}
        canUpgrade={opts.canUpgrade ?? true}
      />
    </QueryClientProvider>,
  );
}

describe("AgentCliSection", () => {
  beforeEach(() => {
    mockInitiate.mockReset();
    mockGetJob.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders current, latest, and status for each managed CLI", () => {
    renderSection([
      row({
        cli: "codex",
        current: "0.9.0",
        latest: "1.0.0",
        status: "outdated",
        upgradable: true,
      }),
      row({
        cli: "claude",
        current: "2.0.0",
        latest: "2.0.0",
        status: "up_to_date",
        upgradable: false,
      }),
      row({
        cli: "grok",
        current: "",
        latest: "",
        status: "missing",
        upgradable: false,
        channel: "unknown",
      }),
    ]);

    expect(screen.getByTestId("agent-cli-card-codex")).toHaveAttribute(
      "data-status",
      "outdated",
    );
    expect(screen.getByText("0.9.0")).toBeInTheDocument();
    expect(screen.getByText("1.0.0")).toBeInTheDocument();
    expect(screen.getByTestId("agent-cli-update-codex")).toBeEnabled();
    expect(screen.getByTestId("agent-cli-card-claude")).toHaveTextContent("Latest");
    expect(screen.getByTestId("agent-cli-card-grok")).toHaveAttribute(
      "data-manual-install",
      "true",
    );
  });

  it("shows manual install copy and no update control when the row is not upgradable", () => {
    renderSection([
      row({
        cli: "codex",
        current: "",
        latest: "",
        status: "missing",
        upgradable: false,
        channel: "unknown",
      }),
    ]);

    const card = screen.getByTestId("agent-cli-card-codex");
    expect(card).toHaveAttribute("data-manual-install", "true");
    expect(card).toHaveTextContent("Install or upgrade manually");
    expect(screen.queryByTestId("agent-cli-update-codex")).not.toBeInTheDocument();
    expect(mockInitiate).not.toHaveBeenCalled();
  });

  it("offers Update for an upgradable row regardless of CLI name", () => {
    renderSection([
      row({
        cli: "grok",
        current: "0.1.0",
        latest: "0.2.0",
        status: "outdated",
        upgradable: true,
      }),
    ]);

    const card = screen.getByTestId("agent-cli-card-grok");
    expect(card).toHaveAttribute("data-manual-install", "false");
    expect(screen.getByTestId("agent-cli-update-grok")).toBeEnabled();
    expect(card).not.toHaveTextContent("Install or upgrade manually");
  });

  it("keeps an up-to-date row as status only", () => {
    renderSection([
      row({
        cli: "grok",
        current: "1.2.0",
        latest: "1.2.0",
        status: "up_to_date",
        upgradable: false,
      }),
    ]);

    const card = screen.getByTestId("agent-cli-card-grok");
    expect(card).toHaveAttribute("data-manual-install", "false");
    expect(card).toHaveTextContent("Latest");
    expect(card).not.toHaveTextContent("Install or upgrade manually");
    expect(screen.queryByTestId("agent-cli-update-grok")).not.toBeInTheDocument();
  });

  it("hides Update while the runtime is offline", () => {
    renderSection(
      [
        row({
          cli: "codex",
          current: "0.9.0",
          latest: "1.0.0",
          status: "outdated",
          upgradable: true,
        }),
      ],
      { isOnline: false },
    );
    expect(screen.queryByTestId("agent-cli-update-codex")).not.toBeInTheDocument();
  });

  it("shows the draining stage after an upgrade starts", async () => {
    const user = userEvent.setup();
    mockInitiate.mockResolvedValue({
      job_id: "job-1",
      runtime_id: "runtime-1",
      cli: "codex",
      status: "draining",
    });
    mockGetJob.mockResolvedValue({
      job_id: "job-1",
      runtime_id: "runtime-1",
      cli: "codex",
      status: "draining",
    });

    renderSection([
      row({
        cli: "codex",
        current: "0.9.0",
        latest: "1.0.0",
        status: "outdated",
        upgradable: true,
      }),
    ]);

    await user.click(screen.getByTestId("agent-cli-update-codex"));

    await waitFor(() => {
      expect(screen.getByText("Waiting for tasks to finish")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("agent-cli-update-codex")).not.toBeInTheDocument();
    expect(mockInitiate).toHaveBeenCalledWith("runtime-1", "codex");
  });

  it("hides Update when canUpgrade is false", () => {
    renderSection(
      [
        row({
          cli: "codex",
          current: "0.9.0",
          latest: "1.0.0",
          status: "outdated",
          upgradable: true,
        }),
      ],
      { canUpgrade: false },
    );
    expect(screen.queryByTestId("agent-cli-update-codex")).not.toBeInTheDocument();
  });

  it("shows Retry after a failed upgrade and re-initiates on click", async () => {
    const user = userEvent.setup();
    mockInitiate
      .mockRejectedValueOnce(new Error("upgrade blew up"))
      .mockResolvedValueOnce({
        job_id: "job-2",
        runtime_id: "runtime-1",
        cli: "codex",
        status: "draining",
      });
    mockGetJob.mockResolvedValue({
      job_id: "job-2",
      runtime_id: "runtime-1",
      cli: "codex",
      status: "draining",
    });

    renderSection([
      row({
        cli: "codex",
        current: "0.9.0",
        latest: "1.0.0",
        status: "outdated",
        upgradable: true,
      }),
    ]);

    await user.click(screen.getByTestId("agent-cli-update-codex"));

    const retry = await screen.findByTestId("agent-cli-retry-codex");
    expect(screen.getByText("upgrade blew up")).toBeInTheDocument();

    await user.click(retry);

    await waitFor(() => {
      expect(mockInitiate).toHaveBeenCalledTimes(2);
    });
    expect(mockInitiate).toHaveBeenNthCalledWith(2, "runtime-1", "codex");
    await waitFor(() => {
      expect(screen.getByText("Waiting for tasks to finish")).toBeInTheDocument();
    });
  });
});
