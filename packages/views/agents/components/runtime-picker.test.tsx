// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import enIssues from "../../locales/en/issues.json";

vi.mock("../../runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

import {
  CLOUD_PREVIEW_RUNTIME_ID,
  cloudPreviewStore,
  resetCloudPreview,
} from "../../runtimes/components/cloud-preview";
import { RuntimePicker } from "./runtime-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};
const ME = "user-me";
const MEMBERS = [
  { user_id: ME, name: "Me", role: "member" },
] as unknown as MemberWithUser[];

function makeRuntime(overrides: Partial<RuntimeDevice>): RuntimeDevice {
  return {
    id: "rt",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (host.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "host.local · macOS (arm64)",
    metadata: {},
    owner_id: ME,
    visibility: "private",
    last_seen_at: "2026-07-11T00:00:00Z",
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
    ...overrides,
  } as RuntimeDevice;
}

const RUNTIMES = [
  makeRuntime({ id: "rt-claude" }),
  makeRuntime({
    id: "rt-codex",
    name: "Codex (host.local)",
    provider: "codex",
  }),
];

function renderPicker(
  props: Partial<React.ComponentProps<typeof RuntimePicker>> = {},
) {
  const onSelect = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <RuntimePicker
        runtimes={RUNTIMES}
        members={MEMBERS}
        currentUserId={ME}
        selectedRuntimeId=""
        onSelect={onSelect}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onSelect };
}

describe("RuntimePicker hierarchical execution flow", () => {
  beforeEach(() => {
    cleanup();
    resetCloudPreview();
  });
  afterEach(() => {
    cleanup();
    resetCloudPreview();
  });

  it("starts with device selection and includes Multica Cloud", () => {
    renderPicker();

    expect(screen.getByText("Choose a device")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Multica Cloud/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("Not turned on")).toBeInTheDocument();
    expect(screen.queryByText("Choose a runtime")).toBeNull();
  });

  it("asks to turn on Cloud before opening the second level", () => {
    renderPicker();

    fireEvent.click(screen.getByRole("button", { name: /Multica Cloud/ }));

    expect(
      screen.getByRole("alertdialog", { name: "Turn on Multica Cloud?" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Choose a runtime")).toBeNull();
    expect(cloudPreviewStore.getState().cloudOn).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(screen.getByText("Choose a device")).toBeInTheDocument();
  });

  it("turns on Cloud and continues to runtime selection", () => {
    const { onSelect } = renderPicker();

    fireEvent.click(screen.getByRole("button", { name: /Multica Cloud/ }));
    fireEvent.click(
      screen.getByRole("button", { name: "Turn on and continue" }),
    );

    expect(
      screen.getByRole("radiogroup", { name: "Choose a runtime" }),
    ).toBeInTheDocument();
    expect(cloudPreviewStore.getState().cloudOn).toBe(true);
    expect(screen.getByRole("radio", { name: /Codex/ })).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: /Claude Code/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: /Gemini CLI/ }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("radio", { name: /Codex/ }));
    expect(onSelect).toHaveBeenLastCalledWith(CLOUD_PREVIEW_RUNTIME_ID);
  });

  it("opens Cloud directly when it is already turned on", () => {
    cloudPreviewStore.getState().setCloudOn(true);
    renderPicker();

    fireEvent.click(screen.getByRole("button", { name: /Multica Cloud/ }));

    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(
      screen.getByRole("radiogroup", { name: "Choose a runtime" }),
    ).toBeInTheDocument();
  });

  it("keeps runtimes scoped to the selected computer", () => {
    renderPicker({ selectedRuntimeId: "rt-claude" });

    expect(
      screen.getByRole("radiogroup", { name: "Choose a runtime" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /Claude/ })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /Codex/ })).toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: /Gemini CLI/ })).toBeNull();
  });

  it("clears the runtime when returning to device selection", () => {
    const { onSelect } = renderPicker({ selectedRuntimeId: "rt-claude" });

    fireEvent.click(
      screen.getByRole("button", { name: "Choose a different device" }),
    );

    expect(onSelect).toHaveBeenCalledWith("");
  });

  it("locks both navigation and runtime selection while disabled", () => {
    const { onSelect } = renderPicker({
      selectedRuntimeId: "rt-claude",
      disabled: true,
    });

    expect(
      screen.getByRole("button", { name: "Choose a different device" }),
    ).toBeDisabled();
    for (const runtime of screen.getAllByRole("radio")) {
      expect(runtime).toBeDisabled();
      fireEvent.click(runtime);
    }
    expect(onSelect).not.toHaveBeenCalled();
  });
});
