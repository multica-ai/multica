// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const picker = vi.hoisted(() => ({
  props: null as null | { runtimes: AgentRuntime[]; canEdit?: boolean },
}));
const mockInitiateListModels = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    initiateListModels: (...args: unknown[]) => mockInitiateListModels(...args),
    getListModelsResult: vi.fn(),
  },
}));
vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div data-testid="avatar-upload" />,
}));
vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: (props: { runtimes: AgentRuntime[]; canEdit?: boolean }) => {
    picker.props = props;
    return <div data-testid="runtime-picker" />;
  },
}));

import { AgentDetailInspector } from "./agent-detail-inspector";

function runtime(id: string, provider: string): AgentRuntime {
  return {
    id,
    workspace_id: "workspace-1",
    daemon_id: null,
    name: id,
    runtime_mode: provider === "qoder_cloud" ? "cloud" : "local",
    provider,
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-1",
    visibility: "public",
    last_seen_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const claude = runtime("rt-claude", "claude");
const qoder = runtime("rt-qoder", "qoder_cloud");

function renderInspector(bound: AgentRuntime) {
  const agent = {
    id: "agent-1",
    workspace_id: "workspace-1",
    name: "Lambda",
    description: "",
    runtime_id: bound.id,
    model: "",
    max_concurrent_tasks: 6,
  } as Agent;
  renderWithI18n(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <AgentDetailInspector
        agent={agent}
        runtime={bound}
        runtimes={[claude, qoder]}
        members={[]}
        currentUserId="user-1"
        canEdit
        onUpdate={vi.fn(async () => {})}
      />
    </QueryClientProvider>,
  );
}

describe("AgentDetailInspector and the Qoder runtime", () => {
  beforeEach(() => {
    picker.props = null;
    mockInitiateListModels.mockReset();
  });
  afterEach(cleanup);

  it("locks the runtime and drops model overrides for a Qoder agent", () => {
    renderInspector(qoder);
    expect(picker.props?.canEdit).toBe(false);
    expect(screen.queryByText("Model")).toBeNull();
    expect(
      screen.getByText("Model, tools and skills are set on the QCA agent in Qoder."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Concurrency")).toHaveValue(6);
    expect(mockInitiateListModels).not.toHaveBeenCalled();
  });

  it("does not offer the Qoder runtime to other agents", () => {
    renderInspector(claude);
    expect(picker.props?.canEdit).toBe(true);
    expect(picker.props?.runtimes.map((r) => r.id)).toEqual(["rt-claude"]);
    expect(screen.getByText("Model")).toBeInTheDocument();
  });
});
