// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";

// ActorAvatar pulls workspace context this unit test doesn't provide.
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

// Provider logos are inline SVGs with no behavior under test.
vi.mock("../../../runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

import { RuntimePicker } from "./runtime-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

const ME = "user-me";
const OTHER = "user-other";

const MEMBERS = [
  { user_id: ME, name: "Me", role: "member" },
  { user_id: OTHER, name: "Other", role: "member" },
] as unknown as MemberWithUser[];

function makeRuntime(overrides: Partial<RuntimeDevice>): RuntimeDevice {
  return {
    id: "rt",
    workspace_id: "ws-1",
    daemon_id: null,
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
  };
}

// Machine "Jiayuan's MacBook Pro": a machine-level rename stamped the same
// custom_name on both runtimes (MUL-4217) — the exact shape that made the
// old flat list unreadable.
const RT_CLAUDE = makeRuntime({
  id: "rt-claude",
  daemon_id: "daemon-1",
  name: "Claude (mbp.local)",
  custom_name: "Jiayuan's MacBook Pro",
  provider: "claude",
});
const RT_CODEX = makeRuntime({
  id: "rt-codex",
  daemon_id: "daemon-1",
  name: "Codex (mbp.local)",
  custom_name: "Jiayuan's MacBook Pro",
  provider: "codex",
});

// Another member's public machine.
const RT_OTHER_PUBLIC = makeRuntime({
  id: "rt-other-claude",
  daemon_id: "daemon-2",
  name: "Claude (other.local)",
  owner_id: OTHER,
  visibility: "public",
});

// Another member's private machine — visible in All but locked.
const RT_OTHER_PRIVATE = makeRuntime({
  id: "rt-other-private",
  daemon_id: "daemon-3",
  name: "Gemini (secret.local)",
  provider: "gemini",
  owner_id: OTHER,
  visibility: "private",
});

const ALL_RUNTIMES = [RT_CLAUDE, RT_CODEX, RT_OTHER_PUBLIC, RT_OTHER_PRIVATE];

function renderPicker(
  props: Partial<React.ComponentProps<typeof RuntimePicker>> = {},
) {
  const onChange = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <RuntimePicker
        variant="field"
        showLabel={false}
        value="rt-claude"
        runtimes={ALL_RUNTIMES}
        members={MEMBERS}
        currentUserId={ME}
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

function openPicker() {
  fireEvent.click(screen.getByRole("button", { name: /^Runtime · / }));
}

describe("RuntimePicker (agent settings)", () => {
  beforeEach(() => cleanup());
  afterEach(() => cleanup());

  it("labels the trigger with the runtime, not just the machine", () => {
    renderPicker();
    // Runtime identity first ("Claude"), machine second — previously the
    // trigger was just the machine name.
    expect(
      screen.getByRole("button", {
        name: /Runtime · Claude · Jiayuan's MacBook Pro · online/,
      }),
    ).toBeTruthy();
  });

  it("opens inside the selected runtime's machine with runtime-labelled rows", () => {
    renderPicker();
    openPicker();

    // Level 2: rows are labelled by runtime, not by the machine rename.
    expect(screen.getByRole("button", { name: /^Claude/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Codex/ })).toBeTruthy();
    // Back affordance carries the machine context.
    expect(
      screen.getByRole("button", { name: "Back to machines" }),
    ).toBeTruthy();
    // Other machines are not mixed into this machine's list.
    expect(screen.queryByText("other.local")).toBeNull();
  });

  it("navigates back to the machine list and scopes it with Mine/All", () => {
    renderPicker();
    openPicker();
    fireEvent.click(screen.getByRole("button", { name: "Back to machines" }));

    // The picker defaults to Mine when "mine" has a runtime
    // (here rt-claude), so only my machine lists up front; the All fallback
    // only kicks in when Mine would be empty.
    expect(
      screen.getByRole("button", { name: /^Jiayuan's MacBook Pro/ }),
    ).toBeTruthy();
    expect(screen.getByText("2/2 online")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^other\.local/ })).toBeNull();

    // All scope: every machine lists, including other members'.
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    expect(
      screen.getByRole("button", { name: /^other\.local/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /^secret\.local/ }),
    ).toBeTruthy();
  });

  it("drills into another machine and selects a runtime on it", async () => {
    const { onChange } = renderPicker();
    openPicker();
    fireEvent.click(screen.getByRole("button", { name: "Back to machines" }));
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    fireEvent.click(screen.getByRole("button", { name: /^other\.local/ }));

    const row = await screen.findByRole("button", { name: /^Claude/ });
    fireEvent.click(row);
    expect(onChange).toHaveBeenCalledWith("rt-other-claude");
  });

  it("widens to the All scope when the selection belongs to someone else", () => {
    renderPicker({ value: "rt-other-claude" });
    openPicker();

    // Lands inside the other member's machine even though the picker
    // defaults to the Mine scope.
    expect(screen.getByRole("button", { name: /^Claude/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Back to machines" }));
    expect(
      screen.getByRole("button", { name: /^Jiayuan's MacBook Pro/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /^other\.local/ }),
    ).toBeTruthy();
  });

  it("keeps other members' private runtimes locked", () => {
    const { onChange } = renderPicker();
    openPicker();
    fireEvent.click(screen.getByRole("button", { name: "Back to machines" }));
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    fireEvent.click(screen.getByRole("button", { name: /^secret\.local/ }));

    const locked = screen.getByRole("button", { name: /^Gemini/ });
    expect((locked as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(locked);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("shows the machine list when nothing is selected and several machines exist", () => {
    renderPicker({
      value: "",
      runtimes: [
        RT_CLAUDE,
        RT_CODEX,
        makeRuntime({
          id: "rt-laptop",
          daemon_id: "daemon-4",
          name: "Claude (laptop.local)",
        }),
      ],
    });
    fireEvent.click(screen.getByRole("button", { name: /^Runtime · none/ }));

    expect(
      screen.getByRole("button", { name: /^Jiayuan's MacBook Pro/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /^laptop\.local/ }),
    ).toBeTruthy();
    // Level 1 lists machines only — no runtime rows yet.
    expect(screen.queryByRole("button", { name: /^Codex/ })).toBeNull();
  });

  it("skips the pointless single-machine list when nothing is selected", () => {
    renderPicker({ value: "", runtimes: [RT_CLAUDE, RT_CODEX] });
    fireEvent.click(screen.getByRole("button", { name: /^Runtime · none/ }));

    expect(screen.getByRole("button", { name: /^Claude/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Codex/ })).toBeTruthy();
  });
});

// The default scope is a pure function of three independent inputs: do I own a
// runtime, does another member expose a *pickable* (public) one, and is another
// member's runtime present but locked (private). "Usable" == pickable: a locked
// private runtime never counts toward All. This covers the full 2×2×2 product.
describe("RuntimePicker default scope (mine/all derivation)", () => {
  // Open the picker and normalise to the machine list — the picker auto-drills
  // into a lone machine, so step back out when it does — then report the active
  // scope. "none" means the scope toggle isn't rendered (no other member's
  // runtime exists to scope against).
  function openToMachineList(): "mine" | "all" | "none" {
    fireEvent.click(screen.getByRole("button", { name: /^Runtime · / }));
    const back = screen.queryByRole("button", { name: "Back to machines" });
    if (back) fireEvent.click(back);
    const mine = screen.queryByRole("button", { name: "Mine" });
    const all = screen.queryByRole("button", { name: "All" });
    if (!mine || !all) return "none";
    return mine.className.includes("bg-background") ? "mine" : "all";
  }

  const MINE_MACHINE = /^Jiayuan's MacBook Pro/;
  const PUBLIC_MACHINE = /^other\.local/;
  const PRIVATE_MACHINE = /^secret\.local/;

  const cases: {
    name: string;
    runtimes: RuntimeDevice[];
    scope: "mine" | "all" | "none";
    visible: RegExp[];
    hidden: RegExp[];
  }[] = [
    {
      name: "no runtimes → mine, no toggle, empty list",
      runtimes: [],
      scope: "none",
      visible: [],
      hidden: [MINE_MACHINE, PUBLIC_MACHINE, PRIVATE_MACHINE],
    },
    {
      name: "mine only → mine, no toggle, my machine lists",
      runtimes: [RT_CLAUDE],
      scope: "none",
      visible: [MINE_MACHINE],
      hidden: [PUBLIC_MACHINE, PRIVATE_MACHINE],
    },
    {
      name: "others' public only → all, their pickable machine lists",
      runtimes: [RT_OTHER_PUBLIC],
      scope: "all",
      visible: [PUBLIC_MACHINE],
      hidden: [MINE_MACHINE, PRIVATE_MACHINE],
    },
    {
      name: "others' private only → mine, empty (locked machine hidden)",
      runtimes: [RT_OTHER_PRIVATE],
      scope: "mine",
      visible: [],
      hidden: [MINE_MACHINE, PUBLIC_MACHINE, PRIVATE_MACHINE],
    },
    {
      name: "others' public + private → all, both machines list",
      runtimes: [RT_OTHER_PUBLIC, RT_OTHER_PRIVATE],
      scope: "all",
      visible: [PUBLIC_MACHINE, PRIVATE_MACHINE],
      hidden: [MINE_MACHINE],
    },
    {
      name: "mine + others' public → mine, others hidden in mine scope",
      runtimes: [RT_CLAUDE, RT_OTHER_PUBLIC],
      scope: "mine",
      visible: [MINE_MACHINE],
      hidden: [PUBLIC_MACHINE, PRIVATE_MACHINE],
    },
    {
      name: "mine + others' private → mine, locked machine hidden",
      runtimes: [RT_CLAUDE, RT_OTHER_PRIVATE],
      scope: "mine",
      visible: [MINE_MACHINE],
      hidden: [PUBLIC_MACHINE, PRIVATE_MACHINE],
    },
    {
      name: "mine + others' public + private → mine, others hidden",
      runtimes: [RT_CLAUDE, RT_OTHER_PUBLIC, RT_OTHER_PRIVATE],
      scope: "mine",
      visible: [MINE_MACHINE],
      hidden: [PUBLIC_MACHINE, PRIVATE_MACHINE],
    },
  ];

  for (const c of cases) {
    it(c.name, () => {
      renderPicker({ value: "", runtimes: c.runtimes });
      expect(openToMachineList()).toBe(c.scope);
      for (const re of c.visible) {
        expect(screen.getByRole("button", { name: re })).toBeTruthy();
      }
      for (const re of c.hidden) {
        expect(screen.queryByRole("button", { name: re })).toBeNull();
      }
    });
  }
});
