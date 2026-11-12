// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, cleanup, within } from "@testing-library/react";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import enIssues from "../../locales/en/issues.json";
import enRuntimes from "../../locales/en/runtimes.json";

// ActorAvatar pulls workspace context this unit test doesn't provide.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

// Provider logos are inline SVGs with no behavior under test.
vi.mock("../../runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

import { RuntimePicker } from "./runtime-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues, runtimes: enRuntimes },
};

const ME = "user-me";

const MEMBERS = [{ user_id: ME, name: "Me", role: "member" }] as unknown as MemberWithUser[];

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
  makeRuntime({ id: "rt-a", name: "Claude (a.local)" }),
  makeRuntime({ id: "rt-b", name: "Claude (b.local)", provider: "codex" }),
];

function renderPicker(props: Partial<React.ComponentProps<typeof RuntimePicker>> = {}) {
  const onSelect = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <RuntimePicker
        runtimes={RUNTIMES}
        members={MEMBERS}
        currentUserId={ME}
        selectedRuntimeId="rt-a"
        onSelect={onSelect}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onSelect };
}

function trigger(container: HTMLElement): HTMLButtonElement {
  const element = container.querySelector<HTMLButtonElement>('[data-slot="popover-trigger"]');
  if (!element) throw new Error("runtime picker trigger not rendered");
  return element;
}

function candidates() {
  const popup = document.querySelector<HTMLElement>('[data-slot="popover-content"]');
  if (!popup) throw new Error("runtime picker list not rendered");
  return within(popup);
}

describe("RuntimePicker (creation studio)", () => {
  beforeEach(() => cleanup());
  afterEach(() => cleanup());

  it("identifies an inherited public runtime without changing the selection", () => {
    const shared = makeRuntime({ id: "rt-shared", owner_id: "user-other", visibility: "public" });
    const { container, onSelect } = renderPicker({
      runtimes: [...RUNTIMES, shared],
      selectedRuntimeId: shared.id,
    });
    expect(trigger(container).textContent).toContain("Public");
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("shows the selected owner's private runtime visibility", () => {
    const { container, onSelect } = renderPicker();
    expect(trigger(container).textContent).toContain("Private");
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("labels a public candidate and still selects it through the normal picker", () => {
    const shared = makeRuntime({ id: "rt-shared", name: "Shared runtime", visibility: "public" });
    const { container, onSelect } = renderPicker({ runtimes: [...RUNTIMES, shared] });
    fireEvent.click(trigger(container));
    const candidate = candidates().getByRole("button", { name: /Shared runtime.*Public/ });
    expect(candidate).not.toBeDisabled();
    fireEvent.click(candidate);
    expect(onSelect).toHaveBeenCalledWith(shared.id);
  });

  it("identifies an inherited runtime shared with me and allows selecting it", () => {
    const shared = makeRuntime({
      id: "rt-members",
      name: "Alice shared runtime",
      owner_id: "user-other",
      shared_with: [ME],
    });
    const { container, getByRole, onSelect } = renderPicker({
      runtimes: [...RUNTIMES, shared],
      selectedRuntimeId: shared.id,
    });
    expect(trigger(container).textContent).toContain("Specific members");
    expect(onSelect).not.toHaveBeenCalled();

    fireEvent.click(getByRole("button", { name: "All" }));
    onSelect.mockClear();
    fireEvent.click(trigger(container));
    const candidate = candidates().getByRole("button", { name: /Alice shared runtime.*Specific members/ });
    expect(candidate).not.toHaveAttribute("aria-disabled");
    fireEvent.click(candidate);
    expect(onSelect).toHaveBeenCalledWith(shared.id);
  });

  it("labels usable private candidates and keeps ungranted runtimes locked", () => {
    const locked = makeRuntime({
      id: "rt-locked",
      name: "Alice locked runtime",
      owner_id: "user-other",
      shared_with: ["someone-else"],
    });
    const { container, getByRole, onSelect } = renderPicker({
      runtimes: [...RUNTIMES, locked],
    });
    fireEvent.click(getByRole("button", { name: "All" }));
    onSelect.mockClear();
    fireEvent.click(trigger(container));
    for (const candidate of candidates().getAllByRole("button", { name: /Claude.*Private/ })) {
      expect(candidate).not.toHaveAttribute("aria-disabled");
    }
    const candidate = candidates().getByRole("button", { name: /Alice locked runtime.*Private/ });
    expect(candidate).toHaveAttribute("aria-disabled", "true");
    expect(candidate.textContent).not.toContain("Specific members");
    fireEvent.click(candidate);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("keeps an overlong runtime name ellipsized inside the trigger instead of overflowing", () => {
    // Same flex-row truncation contract as ModelDropdown: the label needs
    // min-w-0 or it overflows the picker when names grow long.
    const longName = `Claude (${"very-long-host-name-".repeat(12)}.local)`;
    const runtimes = [makeRuntime({ id: "rt-long", name: longName })];
    const { container } = renderPicker({ runtimes, selectedRuntimeId: "rt-long" });
    const label = trigger(container).querySelector("span.truncate");
    expect(label?.textContent).toBe(longName);
    expect(label?.className).toContain("min-w-0");
  });

  it("opens the runtime list on click", () => {
    const { container } = renderPicker();
    fireEvent.click(trigger(container));
    expect(trigger(container).getAttribute("aria-expanded")).toBe("true");
  });

  // A builder session rebinds its execution runtime on the server. While that
  // request is in flight the selection cannot be honoured yet, so the picker
  // must refuse to open rather than let the user queue a second, conflicting
  // choice (MUL-5163).
  it("cannot be opened while disabled", () => {
    const { container, onSelect } = renderPicker({ disabled: true });
    expect(trigger(container).disabled).toBe(true);
    fireEvent.click(trigger(container));
    expect(trigger(container).getAttribute("aria-expanded")).not.toBe("true");
    expect(onSelect).not.toHaveBeenCalled();
  });

  // Switching the Mine/All tab re-selects the first usable runtime in the new
  // list, so it is a second path into onSelect. Leaving it live while disabled
  // would let a locked picker start a switch the server then has to refuse.
  it("does not select through the Mine/All filter while disabled", () => {
    const { container, onSelect } = renderPicker({
      runtimes: [
        ...RUNTIMES,
        makeRuntime({ id: "rt-other", name: "Claude (other.local)", owner_id: "user-other", visibility: "public" }),
      ],
      disabled: true,
    });
    const filters = [...container.querySelectorAll("button")].filter(
      (button) => button.getAttribute("data-slot") !== "popover-trigger",
    );
    expect(filters.length).toBeGreaterThan(0);
    for (const button of filters) {
      expect(button.disabled).toBe(true);
      fireEvent.click(button);
    }
    expect(onSelect).not.toHaveBeenCalled();
  });
});
