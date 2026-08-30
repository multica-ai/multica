// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentDetailInspector } from "./agent-detail-inspector";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: undefined, isSuccess: false }),
}));
vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div />,
}));
vi.mock("./inspector/model-picker", () => ({
  ModelPicker: () => <div />,
}));
vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: () => <div />,
}));
vi.mock("./inspector/thinking-prop-row", () => ({
  ThinkingSettingField: () => <div />,
}));
vi.mock("./inspector/service-tier-setting-field", () => ({
  ServiceTierSettingField: () => <div />,
}));

const agent = {
  id: "agent-1",
  workspace_id: "workspace-1",
  name: "Lambda",
  description: "Test agent",
  runtime_id: "runtime-1",
  operating_mode: "coding",
  max_concurrent_tasks: 6,
} as Agent;

describe("AgentDetailInspector operating mode", () => {
  afterEach(cleanup);

  it("persists a mode change through the existing update callback", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn(async () => {});
    renderWithI18n(
      <AgentDetailInspector
        agent={agent}
        runtime={null}
        runtimes={[]}
        members={[]}
        currentUserId="user-1"
        canEdit
        onUpdate={onUpdate}
      />,
    );

    await user.click(screen.getByRole("radio", { name: /Operational/ }));
    expect(onUpdate).toHaveBeenCalledWith("agent-1", {
      operating_mode: "operational",
    });
  });
});
