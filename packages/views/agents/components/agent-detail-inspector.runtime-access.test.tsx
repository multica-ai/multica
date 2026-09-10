// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, waitFor } from "@testing-library/react";
import type {
  Agent,
  AgentRuntime,
  RuntimeModelListRequest,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockInitiateListModels = vi.hoisted(() => vi.fn());
const mockGetListModelsResult = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    initiateListModels: (...args: unknown[]) =>
      mockInitiateListModels(...args),
    getListModelsResult: (...args: unknown[]) =>
      mockGetListModelsResult(...args),
  },
}));

vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div data-testid="avatar-upload" />,
}));

vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: () => <div data-testid="runtime-picker" />,
}));

import { AgentDetailInspector } from "./agent-detail-inspector";

const agent = {
  id: "agent-1",
  workspace_id: "workspace-1",
  name: "Lambda",
  description: "Test agent",
  runtime_id: "runtime-1",
} as Agent;

const privateRuntime = {
  id: "runtime-1",
  workspace_id: "workspace-1",
  daemon_id: "daemon-1",
  name: "Private runtime",
  runtime_mode: "local",
  provider: "codex",
  launch_header: "",
  status: "online",
  device_info: "Mac",
  metadata: {},
  owner_id: "user-2",
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} satisfies AgentRuntime;

const completedModelsRequest = {
  id: "request-1",
  runtime_id: privateRuntime.id,
  status: "completed",
  models: [],
  supported: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} satisfies RuntimeModelListRequest;

let queryClient: QueryClient;

function renderInspector(currentUserId: string) {
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <AgentDetailInspector
        agent={agent}
        runtime={privateRuntime}
        runtimes={[privateRuntime]}
        members={[]}
        currentUserId={currentUserId}
        canEdit
        onUpdate={vi.fn(async () => {})}
      />
    </QueryClientProvider>,
  );
}

describe("AgentDetailInspector runtime access", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInitiateListModels.mockResolvedValue(completedModelsRequest);
    mockGetListModelsResult.mockResolvedValue(completedModelsRequest);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    cleanup();
    queryClient.clear();
  });

  it("does not discover models for another member's private runtime", async () => {
    renderInspector("admin-1");

    await act(async () => {
      await Promise.resolve();
    });

    expect(mockInitiateListModels).not.toHaveBeenCalled();
  });

  it("still discovers models for the private runtime owner", async () => {
    renderInspector(privateRuntime.owner_id);

    await waitFor(() => {
      expect(mockInitiateListModels).toHaveBeenCalledWith(privateRuntime.id);
    });
  });
});
