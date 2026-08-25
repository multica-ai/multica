import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentDetailInspector } from "./agent-detail-inspector";

const mockUseQuery = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: (options: unknown) => {
    mockUseQuery(options);
    return { data: undefined, isSuccess: false };
  },
}));

// The picker is the only way agent labels ever reached the agent settings
// form. Mocking it means the assertion below fails loudly if the row is
// re-added, instead of passing because a real picker rendered nothing.
vi.mock("../../labels/resource-label-picker", () => ({
  ResourceLabelPicker: () => <div data-testid="resource-label-picker" />,
}));

vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div data-testid="avatar-upload" />,
}));

vi.mock("./inspector/model-picker", () => ({
  ModelPicker: () => <div data-testid="model-picker" />,
}));

vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: () => <div data-testid="runtime-picker" />,
}));

vi.mock("./inspector/thinking-prop-row", () => ({
  ThinkingSettingField: () => <div data-testid="thinking-field" />,
}));

vi.mock("./inspector/service-tier-setting-field", () => ({
  ServiceTierSettingField: () => <div data-testid="service-tier-field" />,
}));

const agent = {
  id: "agent-1",
  workspace_id: "workspace-1",
  name: "Lambda",
  description: "Test agent",
  runtime_id: "runtime-1",
} as Agent;

describe("AgentDetailInspector labels", () => {
  afterEach(() => {
    cleanup();
    mockUseQuery.mockClear();
  });

  // Agent labels were removed from the product (MUL-5600). Label Settings no
  // longer manages an agent catalog, so an attach-only picker here would be a
  // dead end pointing at a catalog the user cannot populate.
  it("does not offer a label picker", () => {
    renderWithI18n(
      <AgentDetailInspector
        agent={agent}
        runtime={null}
        runtimes={[]}
        members={[]}
        currentUserId="user-1"
        canEdit
        onUpdate={vi.fn(async () => {})}
      />,
    );

    // Sanity check: the profile card actually rendered.
    expect(screen.getByLabelText("Name")).toBeInTheDocument();

    expect(screen.queryByTestId("resource-label-picker")).toBeNull();
    expect(screen.queryByText("Labels")).toBeNull();
  });

  it("does not discover models for another member's private runtime", () => {
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

    renderWithI18n(
      <AgentDetailInspector
        agent={agent}
        runtime={privateRuntime}
        runtimes={[privateRuntime]}
        members={[]}
        currentUserId="admin-1"
        canEdit
        onUpdate={vi.fn(async () => {})}
      />,
    );

    expect(mockUseQuery).toHaveBeenCalledWith(
      expect.objectContaining({ enabled: false }),
    );
  });
});
