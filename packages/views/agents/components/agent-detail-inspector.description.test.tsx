// @vitest-environment jsdom

// The description counter, the autosave gate and the
// server all count Unicode code points. 128 emoji are 256 UTF-16 units, which
// the old `description.length` gate refused to save while the counter read
// 128 / 255.

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentDetailInspector } from "./agent-detail-inspector";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: undefined, isSuccess: false }),
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
  description: "Coding agent",
  runtime_id: "runtime-1",
  avatar_url: null,
  conversation_starters: [],
  max_concurrent_tasks: 1,
} as unknown as Agent;

function renderInspector({
  runtime = null,
}: { runtime?: AgentRuntime | null } = {}) {
  const onUpdate = vi.fn(
    async (_id: string, _data: Record<string, unknown>) => {},
  );
  renderWithI18n(
    <AgentDetailInspector
      agent={agent}
      runtime={runtime}
      runtimes={[]}
      members={[]}
      currentUserId="user-1"
      canEdit
      onUpdate={onUpdate}
    />,
  );
  return onUpdate;
}

describe("AgentDetailInspector description limit", () => {
  afterEach(() => {
    cleanup();
  });

  it("saves 128 emoji: counter and autosave both count code points", async () => {
    const onUpdate = renderInspector();
    const value = "😀".repeat(128);

    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value },
    });

    expect(screen.getByText("128 / 255")).toBeInTheDocument();
    await waitFor(
      () =>
        expect(onUpdate).toHaveBeenCalledWith("agent-1", {
          description: value,
        }),
      { timeout: 2000 },
    );
  });

  it("does not save 256 emoji and says by how much it is over", async () => {
    const onUpdate = renderInspector();

    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "😀".repeat(256) },
    });

    expect(screen.getByText(/256 \/ 255/)).toHaveClass("text-destructive");
    await new Promise((resolve) => setTimeout(resolve, 1200));
    expect(onUpdate).not.toHaveBeenCalled();
  });

  it("has no UTF-16 maxLength on the textarea", () => {
    renderInspector();
    expect(screen.getByLabelText("Description")).not.toHaveAttribute("maxlength");
  });
});
