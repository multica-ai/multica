// @vitest-environment jsdom

import { describe, expect, it, beforeEach, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { RuntimeProfile } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const queryState = vi.hoisted(() => ({
  profiles: [] as RuntimeProfile[],
  isLoading: false,
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: vi.fn(() => ({
      data: queryState.profiles,
      isLoading: queryState.isLoading,
    })),
  };
});

vi.mock("@multica/core/runtimes", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/runtimes")>(
      "@multica/core/runtimes",
    );
  return {
    ...actual,
    // The form hooks normally pull `useQueryClient`, which the test
    // harness deliberately does NOT install (we want unit-level isolation
    // from the real React Query cache). Stub the mutation interface to
    // its smallest usable shape so the form component renders.
    useCreateRuntimeProfile: () => ({
      mutateAsync: vi.fn(),
      isPending: false,
    }),
    useUpdateRuntimeProfile: () => ({
      mutateAsync: vi.fn(),
      isPending: false,
    }),
  };
});

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("./delete-runtime-profile-dialog", () => ({
  DeleteRuntimeProfileDialog: () => null,
}));

vi.mock("./provider-logo", () => ({
  ProviderLogo: () => null,
}));

import { RuntimeProfilesDialog } from "./runtime-profiles-dialog";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

function profile(overrides: Partial<RuntimeProfile> = {}): RuntimeProfile {
  return {
    id: "prof-1",
    workspace_id: "ws-1",
    display_name: "Team Codex",
    protocol_family: "codex",
    command_name: "codex",
    description: null,
    fixed_args: [],
    visibility: "workspace",
    created_by: "user-1",
    enabled: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    ...overrides,
  };
}

function renderDialog() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <RuntimeProfilesDialog wsId="ws-1" onClose={vi.fn()} />
    </I18nProvider>,
  );
}

describe("RuntimeProfilesDialog", () => {
  beforeEach(() => {
    queryState.profiles = [];
    queryState.isLoading = false;
    vi.clearAllMocks();
  });

  it("shows the custom empty state and keeps built-in protocols collapsed", () => {
    renderDialog();

    expect(
      screen.getByText("Create your first custom runtime"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Pick a base protocol family/),
    ).toBeInTheDocument();

    const builtinsToggle = screen.getByRole("button", {
      name: /Supported base protocols/,
    });
    expect(builtinsToggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("claude")).not.toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: "New custom runtime" }),
    ).toHaveLength(2);
  });

  it("renders custom profiles before the collapsed built-in reference section", () => {
    queryState.profiles = [profile()];

    renderDialog();

    const customTitle = screen.getByText("Custom runtimes (1)");
    const customRow = screen.getByText("Team Codex");
    const builtinsToggle = screen.getByRole("button", {
      name: /Supported base protocols/,
    });

    expect(customRow).toBeInTheDocument();
    expect(builtinsToggle).toHaveAttribute("aria-expanded", "false");
    expect(
      customTitle.compareDocumentPosition(builtinsToggle) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.queryByText("claude")).not.toBeInTheDocument();

    fireEvent.click(builtinsToggle);

    expect(builtinsToggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("claude")).toBeInTheDocument();
  });

  it("clears built-in detail when the built-in reference section collapses", () => {
    queryState.profiles = [profile()];

    renderDialog();

    const builtinsToggle = screen.getByRole("button", {
      name: /Supported base protocols/,
    });
    fireEvent.click(builtinsToggle);
    fireEvent.click(screen.getByRole("option", { name: /claude/i }));

    expect(
      screen.getByText(/claude is a built-in protocol family/),
    ).toBeInTheDocument();

    fireEvent.click(builtinsToggle);

    expect(screen.getByText("Select a runtime")).toBeInTheDocument();
    expect(
      screen.queryByText(/claude is a built-in protocol family/),
    ).not.toBeInTheDocument();
  });

  // MUL-3414: the dialog must surface the protocol-family compatibility
  // boundary at exactly the two moments the user is choosing it: at the
  // family-pick step (so they don't pick "claude" intending to launch grok)
  // and at the command field (so they don't type a non-compatible CLI name).
  // The original bug was a runtime that registered, came online, and then
  // failed every task — these hints make the boundary visible before the
  // user gets to that failure mode.
  it("renders the family-compatibility callout on the create-step family picker", () => {
    renderDialog();

    const newButton = screen.getAllByRole("button", {
      name: "New custom runtime",
    })[0];
    if (!newButton) throw new Error("expected a 'New custom runtime' button");
    fireEvent.click(newButton);

    // The callout names the failure mode (`fails every task with empty
    // output`) so the boundary is concrete, not abstract.
    expect(
      screen.getByText(
        /not a generic adapter for arbitrary CLIs/i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/fails every task with empty output/i),
    ).toBeInTheDocument();
  });

  it("renders the command-compatibility hint after picking a family", () => {
    renderDialog();

    const newButton = screen.getAllByRole("button", {
      name: "New custom runtime",
    })[0];
    if (!newButton) throw new Error("expected a 'New custom runtime' button");
    fireEvent.click(newButton);
    // Pick the cursor family — that's the original bug's protocol_family.
    fireEvent.click(screen.getByRole("radio", { name: /cursor/i }));

    // The hint interpolates the chosen family so the user reads
    // "Must accept cursor's launch arguments…" and immediately knows the
    // boundary applies to the family they just picked.
    expect(
      screen.getByText(/Must accept cursor's launch arguments/i),
    ).toBeInTheDocument();
    // grok / droid are named explicitly because those are the exact CLIs
    // the original GitHub bug report tried to drop in.
    expect(
      screen.getByText(/grok or droid don't and need a first-class provider/i),
    ).toBeInTheDocument();
  });
});
