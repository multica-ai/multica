// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EMPTY_AGENT_DRAFT } from "@multica/core/agents";
import { renderWithI18n } from "../../test/i18n";
import { AgentConfigurationPanel } from "./agent-configuration-panel";

vi.mock("@multica/core/config", () => ({
  useConfigStore: () => false,
}));
vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div />,
}));
vi.mock("../components/model-dropdown", () => ({
  ModelDropdown: () => <div />,
}));
vi.mock("../components/runtime-picker", () => ({
  RuntimePicker: () => <div />,
}));
vi.mock("../components/skill-multi-select", () => ({
  SkillMultiSelect: () => <div />,
}));
vi.mock("../components/inspector/thinking-prop-row", () => ({
  ThinkingSettingField: () => <div />,
}));
vi.mock("../components/inspector/service-tier-setting-field", () => ({
  ServiceTierSettingField: () => <div />,
}));

describe("AgentConfigurationPanel operating mode", () => {
  afterEach(cleanup);

  it("updates the shared creation draft", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    renderWithI18n(
      <AgentConfigurationPanel
        draft={{ ...EMPTY_AGENT_DRAFT, name: "Researcher" }}
        onChange={onChange}
        runtimes={[]}
        runtimesLoading={false}
        members={[]}
        currentUserId="user-1"
        nameError={null}
        onNameChange={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("radio", { name: /Operational/ }));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ operatingMode: "operational" }),
    );
  });
});
