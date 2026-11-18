// @vitest-environment jsdom

import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { EMPTY_AGENT_DRAFT, type AgentDraft } from "@multica/core/agents";
import type { RuntimeDevice } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentConfigurationPanel } from "./agent-configuration-panel";

vi.mock("../../common/avatar-upload-control", () => ({ AvatarUploadControl: () => null }));
vi.mock("../components/skill-multi-select", () => ({ SkillMultiSelect: () => <div>skill-picker</div> }));
vi.mock("../components/model-dropdown", () => ({ ModelDropdown: () => <div>model-picker</div> }));
vi.mock("../components/runtime-picker", () => ({
  RuntimePicker: ({ onSelect }: { onSelect: (id: string) => void }) => <button onClick={() => onSelect("kagent-1")}>Choose Kagent</button>,
}));

const runtimes = [
  { id: "codex-1", provider: "codex", status: "offline" },
  { id: "kagent-1", provider: "kagent", status: "online" },
] as RuntimeDevice[];

afterEach(cleanup);

function mount(initial: AgentDraft) {
  const changed = vi.fn();
  function Form() {
    const [draft, setDraft] = useState(initial);
    return <AgentConfigurationPanel
      draft={draft} onChange={(next) => { changed(next); setDraft(next); }}
      runtimes={runtimes} runtimesLoading={false} members={[]}
      currentUserId="owner-1" nameError={null} onNameChange={() => {}}
    />;
  }
  renderWithI18n(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Form /></QueryClientProvider>);
  return changed;
}

describe("Kagent configuration", () => {
  it("hides unsupported controls and explains native-text limits", () => {
    mount({ ...EMPTY_AGENT_DRAFT, runtimeId: "kagent-1" });
    expect(screen.queryByText("model-picker")).not.toBeInTheDocument();
    expect(screen.queryByText("skill-picker")).not.toBeInTheDocument();
    expect(screen.getByText(/Workspace access, skills, model and thinking overrides/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Clear incompatible options" })).not.toBeInTheDocument();
  });

  it("preserves provider options on switch until the explicit clear action", () => {
    const initial = { ...EMPTY_AGENT_DRAFT, runtimeId: "codex-1", model: "old-model", thinkingLevel: "high", serviceTier: "priority", skillIds: new Set(["skill-1"]) };
    const changed = mount(initial);
    fireEvent.click(screen.getByRole("button", { name: "Choose Kagent" }));
    expect(changed).toHaveBeenLastCalledWith({ ...initial, runtimeId: "kagent-1" });
    expect(screen.getByRole("alert")).toHaveTextContent("Creation is blocked");
    expect(screen.queryByText("model-picker")).not.toBeInTheDocument();
    expect(screen.queryByText("skill-picker")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear incompatible options" }));
    expect(changed).toHaveBeenLastCalledWith({ ...initial, runtimeId: "kagent-1", model: "", thinkingLevel: "", serviceTier: "", qoderAgentId: "", skillIds: new Set() });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
