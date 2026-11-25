// @vitest-environment jsdom

import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
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

const initial = {
  id: "agent-1",
  workspace_id: "workspace-1",
  name: "Lambda",
  description: "Old description",
  runtime_id: "runtime-1",
  avatar_url: null,
  conversation_starters: [],
  max_concurrent_tasks: 1,
} as unknown as Agent;

let setAgent: (patch: Partial<Agent>) => void;

function renderInspector(onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>) {
  function Harness() {
    const [agent, set] = useState(initial);
    setAgent = (patch) => act(() => set((current) => ({ ...current, ...patch })));
    return (
      <AgentDetailInspector
        agent={agent}
        runtime={null}
        runtimes={[]}
        members={[]}
        currentUserId="user-1"
        canEdit
        onUpdate={onUpdate}
      />
    );
  }
  renderWithI18n(<Harness />);
}

const advance = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms));

describe("AgentDetailInspector profile", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("saves a rename without writing back a description changed elsewhere", async () => {
    const onUpdate = vi.fn(async () => {});
    renderInspector(onUpdate);

    setAgent({ description: "New description" });
    expect(screen.getByLabelText("Description")).toHaveValue("New description");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Mu" } });
    await advance(1000);

    expect(onUpdate).toHaveBeenCalledExactlyOnceWith("agent-1", { name: "Mu" });
  });

  it("keeps the typed name when an optimistic save is rolled back", async () => {
    renderInspector(async (_id, data) => {
      setAgent(data);
      await new Promise((resolve) => setTimeout(resolve, 100));
      setAgent({ name: "Lambda" });
    });

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Mu" } });
    await advance(1000);

    expect(screen.getByLabelText("Name")).toHaveValue("Mu");
  });
});
