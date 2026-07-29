// @vitest-environment jsdom

// FIR-3212 — Production prompt tab: byte-exact per-run prompt evidence.
// The tab must render recorded evidence only (never a reconstruction),
// show provenance (run, provider+runtime version, config version, sha256),
// and be honest about empty/missing states.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { ...actual.api, cerebroRequest: mockCerebroRequest },
  };
});

import { CerebroAgentPromptSnapshotTab } from "./prompt-snapshot-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Kathrine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: { max_turns: 12, timeout_minutes: 30 },
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const snapshotRef = {
  task_id: "task-1",
  issue_id: "issue-1",
  agent_context_version: "v1.4.0",
  agent_context_version_id: "acv-1",
  provider: "claude",
  model: "claude-sonnet-5",
  runtime_version: "2.1.209",
  system_prompt_mode: "append",
  sha256_original: "a".repeat(64),
  sha256_redacted: "b".repeat(64),
  total_bytes: 93153,
  redacted: true,
  created_at: "2026-07-15T07:32:00Z",
};

const fullSnapshot = {
  ...snapshotRef,
  layers: [
    {
      name: "agent_identity",
      delivery: "workdir_file",
      byte_size: 300,
      sha256_original: "c".repeat(64),
      sha256_redacted: "c".repeat(64),
      content_redacted: "## Agent Identity\nYou are Kathrine.\n",
    },
    {
      name: "task_prompt",
      delivery: "user_prompt",
      byte_size: 120,
      sha256_original: "d".repeat(64),
      sha256_redacted: "d".repeat(64),
      content_redacted: "Fix the login redirect.\n",
    },
  ],
};

function renderTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroAgentPromptSnapshotTab agent={baseAgent} />
    </QueryClientProvider>,
  );
}

async function openAdvanced() {
  const summary = await screen.findByText(
    /Advanced: exact prompt and technical evidence/i,
  );
  await userEvent.click(summary);
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

function mockHappyPath() {
  mockCerebroRequest.mockImplementation((path: string) => {
    if (path.endsWith("/prompt-snapshots")) {
      return Promise.resolve({ snapshots: [snapshotRef] });
    }
    return Promise.resolve(fullSnapshot);
  });
}

describe("CerebroAgentPromptSnapshotTab", () => {
  it("opens on a plain-language summary and keeps raw evidence under Advanced", async () => {
    mockHappyPath();
    renderTab();

    expect(
      await screen.findByRole("region", { name: "Run summary" }),
    ).toBeInTheDocument();
    expect(screen.getByText("What shaped this run")).toBeInTheDocument();
    expect(screen.getByText("Role and boundaries")).toBeInTheDocument();
    expect(screen.getByText("Workspace guidance")).toBeInTheDocument();
    expect(
      screen.getByText("Up to 12 steps · Up to 30 minutes."),
    ).toBeInTheDocument();

    const advanced = screen
      .getByText(/Advanced: exact prompt and technical evidence/i)
      .closest("details");
    expect(advanced).not.toHaveAttribute("open");
    await userEvent.click(
      screen.getByText(/Advanced: exact prompt and technical evidence/i),
    );
    expect(advanced).toHaveAttribute("open");
  });

  it("renders provenance for the recorded run", async () => {
    mockHappyPath();
    renderTab();
    await openAdvanced();

    // Provider + runtime version + config version + hash provenance.
    expect(await screen.findByText(/claude 2\.1\.209/)).toBeInTheDocument();
    expect(screen.getAllByText(/v1\.4\.0/).length).toBeGreaterThan(0);
    // Short sha appears (first 8 hex chars of the redacted-view hash).
    expect(screen.getAllByText(/bbbbbbbb/).length).toBeGreaterThan(0);
    // Evidence badge: captured from the run, never reconstructed.
    expect(
      screen.getByText(/captured from the run — not reconstructed/i),
    ).toBeInTheDocument();
  });

  it("shows the run the snapshot was captured from", async () => {
    mockHappyPath();
    renderTab();
    await openAdvanced();

    // Provenance is only checkable if the reader can tell WHICH run produced
    // this prompt. Without the run id the header describes an anonymous blob.
    await screen.findByText(/claude 2\.1\.209/);
    expect(screen.getAllByText(/task-1/).length).toBeGreaterThan(0);
  });

  it("shows prompt size in tokens alongside bytes", async () => {
    mockHappyPath();
    renderTab();
    await openAdvanced();

    await screen.findByText(/claude 2\.1\.209/);
    // 93153 recorded bytes / 3.8 chars-per-token ≈ 24514 tokens, always
    // presented as an approximation — never as an exact count.
    expect(screen.getByText(/~24,514 tokens/)).toBeInTheDocument();
  });

  it("lists layers with byte sizes and shows layer content with line numbers", async () => {
    mockHappyPath();
    renderTab();
    await openAdvanced();

    expect((await screen.findAllByText("Role & instructions")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("Case itself").length).toBeGreaterThan(0);
    expect(screen.getByText(/You are Kathrine\./)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /case itself/i }));
    expect(screen.getByText(/Fix the login redirect\./)).toBeInTheDocument();
  });

  it("marks user-prompt delivery honestly (no native system prompt claim)", async () => {
    mockHappyPath();
    renderTab();
    await openAdvanced();

    await screen.findAllByText("Case itself");
    await userEvent.click(screen.getByRole("button", { name: /case itself/i }));
    expect(screen.getAllByText(/user message/i).length).toBeGreaterThan(0);
  });

  it("renders an honest empty state when no snapshots exist", async () => {
    mockCerebroRequest.mockResolvedValue({ snapshots: [] });
    renderTab();

    expect(
      await screen.findByText(/no recorded run exists yet/i),
    ).toBeInTheDocument();
  });

  it("keeps pending comparison honest when no proposal exists", async () => {
    mockHappyPath();
    renderTab();

    await screen.findAllByText("Role & instructions");
    const afterTab = screen.getByRole("button", { name: /after pending change/i });
    await userEvent.click(afterTab);
    expect(
      screen.getByText(/no pending configuration change exists/i),
    ).toBeInTheDocument();
  });

  it("shows redaction provenance with both hashes when redacted", async () => {
    mockHappyPath();
    renderTab();
    await openAdvanced();

    await screen.findAllByText("Role & instructions");
    // Both hashes are distinguishable: original vs redacted view.
    expect(screen.getByText(/original/i)).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getAllByText(/aaaaaaaa/).length).toBeGreaterThan(0),
    );
  });
});
