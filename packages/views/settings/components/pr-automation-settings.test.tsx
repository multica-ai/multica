import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { PRAutomationSettings } from "./pr-automation-settings";
const state = vi.hoisted(() => ({
  data: {
    policy: { source: "title_branch", autoComplete: true, revision: 1 },
    migrated: false,
  },
  preview: {
    mutateAsync: vi.fn(),
    isPending: false,
    error: null,
    reset: vi.fn(),
  },
  apply: {
    mutateAsync: vi.fn(),
    isPending: false,
    error: null as Error | null,
    reset: vi.fn(),
  },
  sync: { mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() },
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: state.data }),
}));
vi.mock("@multica/core/github", () => ({
  prPolicyOptions: vi.fn(),
  usePRPolicyMutations: () => state,
}));
function mount(canManage = true) {
  return render(
    <I18nProvider
      locale="en"
      resources={{ en: { common: enCommon, settings: enSettings } }}
    >
      <PRAutomationSettings wsId="workspace" canManage={canManage} />
    </I18nProvider>,
  );
}
const preview = {
  policy: state.data.policy,
  migrated: false,
  issues: [
    {
      id: "i",
      identifier: "MUL-1",
      title: "Deliver",
      added: [],
      removed: [],
      decision: { complete: true },
    },
  ],
  pending: [],
  token: "observed-state",
};
beforeEach(() => {
  vi.clearAllMocks();
  state.apply.error = null;
  state.preview.mutateAsync.mockResolvedValue(preview);
  state.apply.mutateAsync.mockResolvedValue(preview);
});
describe("PR automation settings", () => {
  it("requires preview before saving and sends the reviewed snapshot", async () => {
    const user = userEvent.setup();
    mount();
    expect(
      screen.queryByRole("button", { name: enSettings.pr_automation.save }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: enSettings.pr_automation.preview }),
    );
    expect(await screen.findByText(/MUL-1/)).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: enSettings.pr_automation.save }),
    );
    expect(state.apply.mutateAsync).toHaveBeenCalledWith({
      policy: preview.policy,
      token: "observed-state",
    });
  });
  it("discards a rejected snapshot so stale state cannot be reapplied", async () => {
    state.apply.mutateAsync.mockImplementation(async () => {
      state.apply.error = new Error("PR state changed; preview again");
      throw state.apply.error;
    });
    const user = userEvent.setup();
    mount();
    await user.click(
      screen.getByRole("button", { name: enSettings.pr_automation.preview }),
    );
    await user.click(
      await screen.findByRole("button", {
        name: enSettings.pr_automation.save,
      }),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: enSettings.pr_automation.save }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("preview again");
  });
  it("keeps policy visible without offering writes to a member", () => {
    mount(false);
    expect(screen.getByRole("switch")).toHaveAttribute("aria-disabled", "true");
    expect(
      screen.queryByRole("button", { name: enSettings.pr_automation.preview }),
    ).not.toBeInTheDocument();
  });
});
